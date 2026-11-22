//go:build lab

package isolation

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// TestBenchmark measures the control plane (PRODUCT-PLAN §24.6): BENCH_N
// small Workspaces (one Namespace, a /26 network each from 10.240.128.0/18)
// created at once, how long each takes to be Ready, the API server's request
// count over the run, the operator's and Capsule's peak CPU and memory, and
// how long deleting them all takes. Workloads are not measured.
func TestBenchmark(t *testing.T) {
	n, err := strconv.Atoi(os.Getenv("BENCH_N"))
	if err != nil || n < 1 || n > 60 {
		t.Skip("set BENCH_N to 1..60")
	}
	cs := kubernetes.NewForConfigOrDie(cfg)
	requestsBefore := apiserverRequests(t, cs)
	sampler := startSampler()

	names := make([]string, n)
	created := make([]time.Time, n)
	start := time.Now()
	for i := range n {
		names[i] = fmt.Sprintf("optest-bench-%02d", i)
		ws := workspace(names[i], "01bench", fmt.Sprintf("10.240.%d.%d/26", 128+i/4, (i%4)*64))
		ws.Spec.Namespaces = []string{"app"}
		ws.Spec.Resources = v1.ResourceList{"requests.cpu": resource.MustParse("100m"), "pods": resource.MustParse("2")}
		created[i] = time.Now()
		if err := admin.Create(ctx, ws); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, name := range names {
			_ = admin.Delete(ctx, &v1.PlatformWorkspace{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
	})

	readyAfter := make([]time.Duration, n)
	pending := n
	deadline := time.Now().Add(15 * time.Minute)
	for pending > 0 && time.Now().Before(deadline) {
		var list v1.PlatformWorkspaceList
		if err := admin.List(ctx, &list); err != nil {
			t.Fatal(err)
		}
		for _, ws := range list.Items {
			i := slices.Index(names, ws.Name)
			if i < 0 || readyAfter[i] != 0 || ws.Status.Phase != v1.PhaseReady {
				continue
			}
			readyAfter[i] = time.Since(created[i])
			pending--
		}
		time.Sleep(time.Second)
	}
	allReady := time.Since(start)
	if pending > 0 {
		t.Fatalf("%d of %d Workspaces not Ready after 15 minutes", pending, n)
	}
	requestsAfter := apiserverRequests(t, cs)

	deleteStart := time.Now()
	for _, name := range names {
		if err := admin.Delete(ctx, &v1.PlatformWorkspace{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil && !apierrors.IsNotFound(err) {
			t.Fatal(err)
		}
	}
	waitFor(t, "every Workspace deleted", 15*time.Minute, func() (bool, error) {
		var list v1.PlatformWorkspaceList
		err := admin.List(ctx, &list)
		for _, ws := range list.Items {
			if slices.Contains(names, ws.Name) {
				return false, err
			}
		}
		return true, err
	})
	allDeleted := time.Since(deleteStart)
	peaks := sampler.stop()

	slices.Sort(readyAfter)
	q := func(p float64) time.Duration { return readyAfter[int(p*float64(n-1))].Round(100 * time.Millisecond) }
	t.Logf("BENCH n=%d ready_p50=%s ready_p95=%s ready_max=%s all_ready=%s all_deleted=%s apiserver_requests=%d operator_cpu_peak=%s operator_mem_peak=%s capsule_cpu_peak=%s capsule_mem_peak=%s",
		n, q(0.5), q(0.95), q(1), allReady.Round(time.Second), allDeleted.Round(time.Second), requestsAfter-requestsBefore,
		peaks["operator-cpu"], peaks["operator-mem"], peaks["capsule-cpu"], peaks["capsule-mem"])
}

// apiserverRequests sums apiserver_request_total over the API servers'
// metrics (one API server on the lab).
func apiserverRequests(t *testing.T, cs *kubernetes.Clientset) int64 {
	t.Helper()
	raw, err := cs.RESTClient().Get().AbsPath("/metrics").DoRaw(ctx)
	if err != nil {
		t.Fatalf("API server metrics: %v", err)
	}
	var total float64
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "apiserver_request_total{") {
			continue
		}
		if f, err := strconv.ParseFloat(line[strings.LastIndex(line, " ")+1:], 64); err == nil {
			total += f
		}
	}
	return int64(total)
}

// sampler reads the operator's and Capsule's Pod metrics every 5 seconds
// and keeps the peaks.
type sampler struct {
	mu    sync.Mutex
	peaks map[string]resource.Quantity
	done  chan struct{}
	wg    sync.WaitGroup
}

func startSampler() *sampler {
	s := &sampler{peaks: map[string]resource.Quantity{}, done: make(chan struct{})}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			s.sample("tenantry-operator", "operator")
			s.sample("capsule-system", "capsule")
			select {
			case <-s.done:
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
	return s
}

func (s *sampler) sample(ns, key string) {
	list := &unstructured.UnstructuredList{}
	list.SetAPIVersion("metrics.k8s.io/v1beta1")
	list.SetKind("PodMetricsList")
	if err := admin.List(ctx, list, client.InNamespace(ns)); err != nil {
		return
	}
	for _, pm := range list.Items {
		if key == "capsule" && !strings.HasPrefix(pm.GetName(), "capsule-controller-manager") {
			continue
		}
		containers, _, _ := unstructured.NestedSlice(pm.Object, "containers")
		for _, c := range containers {
			usage, _, _ := unstructured.NestedStringMap(c.(map[string]any), "usage")
			s.keep(key+"-cpu", usage["cpu"])
			s.keep(key+"-mem", usage["memory"])
		}
	}
}

func (s *sampler) keep(key, value string) {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.peaks[key]; !ok || q.Cmp(old) > 0 {
		s.peaks[key] = q
	}
}

func (s *sampler) stop() map[string]string {
	close(s.done)
	s.wg.Wait()
	out := map[string]string{}
	for k, q := range s.peaks {
		if strings.HasSuffix(k, "-cpu") {
			out[k] = fmt.Sprintf("%dm", q.MilliValue())
		} else {
			out[k] = fmt.Sprintf("%dMi", q.Value()>>20)
		}
	}
	return out
}
