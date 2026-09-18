# Kịch bản thuyết trình Tenantry

**Thời lượng mục tiêu:** khoảng 17 phút, chưa tính hỏi đáp.  
**Bộ slide tương ứng:** [Tenantry-v6.pptx](../output/tenantry/Tenantry-v6.pptx) · [Bản PDF](../output/tenantry/Tenantry-v6.pdf)  
**Nội dung đối chiếu:** [SLIDE-CONTENT-DRAFT.md](SLIDE-CONTENT-DRAFT.md)

## Cách sử dụng

- Các đoạn văn và câu **Chuyển slide** là lời nói có thể dùng trực tiếp. Những dòng *Thao tác* chỉ hướng dẫn chỉ vào hình, không đọc thành lời.
- Thời gian gợi ý cho phần nói cộng lại khoảng 16,5 phút, dành khoảng một phút cho ngắt nghỉ và chuyển slide. Đây là mốc tập nói, cần điều chỉnh theo tốc độ thực tế.
- Thay thông tin trong ngoặc vuông ở phần mở đầu. Slide 24 giữ trống như bộ slide hiện tại; khi tập bản này, chuyển từ slide 23 sang slide 25.
- Phần giải pháp trình bày thiết kế đề xuất. Chưa đưa vào lời nói các kết luận về mức tiết kiệm chi phí, tốc độ cấp phát hay hiệu quả cách ly đã được đo đạc.

---



## Slide 01: Tenantry

*Thời gian gợi ý: 25 giây.*

Kính thưa các thầy cô trong hội đồng, em là [Tên tác giả]. Hôm nay, em xin trình bày đồ án Tenantry, với đề tài Kubernetes Tenant as a Service với Capsule, dưới sự hướng dẫn của [Tên giảng viên].

Đồ án đề xuất một dịch vụ cho phép người dùng yêu cầu môi trường Kubernetes, nhận tài nguyên và quyền truy cập trên hạ tầng do bên cung cấp vận hành.

**Chuyển slide:** Sau đây là cấu trúc bài trình bày của em.

## Slide 02: Nội dung trình bày

*Thời gian gợi ý: 15 giây.*

Bài trình bày gồm năm phần như trên slide. Em sẽ bắt đầu từ Kubernetes và nhu cầu sử dụng, xem xét các giải pháp hiện có, sau đó trình bày mô hình dịch vụ và kiến trúc của Tenantry.

**Chuyển slide:** Trước hết, em xin giới thiệu ngắn gọn về Kubernetes.

## Slide 03: Kubernetes

*Thời gian gợi ý: 35 giây.*

Kubernetes là nền tảng mã nguồn mở để quản lý các workload container hóa, được Google mã nguồn mở lần đầu vào năm 2014. Workload ở đây có thể hiểu là ứng dụng hoặc tác vụ mà người dùng muốn chạy.

*Thao tác: Chỉ control plane, rồi chỉ hai worker node trong sơ đồ.*

Một cluster gồm control plane quản lý trạng thái hệ thống và các worker node chạy workload. Người dùng mô tả yêu cầu qua Kubernetes API. Kubernetes hỗ trợ tự động triển khai, mở rộng và phục hồi workload theo cấu hình đó.

**Chuyển slide:** Với những khả năng này, người dùng có thể cần truy cập Kubernetes cho nhiều mục đích khác nhau.

## Slide 04: Nhu cầu truy cập môi trường Kubernetes

*Thời gian gợi ý: 30 giây.*

Người dùng cần truy cập Kubernetes vì nhiều mục đích. Một nhu cầu phổ biến là triển khai và vận hành sản phẩm hoặc dịch vụ lâu dài. Một nhu cầu khác là học Kubernetes và thử nghiệm với các API của nó.

*Thao tác: Chỉ vào khung bên phải.*

Nằm giữa hai nhu cầu đó là việc thực hiện các tác vụ ngắn hạn: xử lý dữ liệu, kiểm thử, chạy thử một phiên bản. Dịch vụ phục vụ cả ba nhu cầu, nhưng em sẽ làm rõ nhu cầu này vì nó hiện chưa có cách đáp ứng thuận tiện.

Điểm chung của cả ba là họ cần một môi trường dùng được, có đủ tài nguyên và quyền truy cập phù hợp.

**Chuyển slide:** Em xin làm rõ nhu cầu ngắn hạn này bằng một số ví dụ cụ thể.

## Slide 05: Nhu cầu sử dụng Kubernetes ngắn hạn

*Thời gian gợi ý: 40 giây.*

Nhiều công việc chỉ cần môi trường Kubernetes trong một thời gian ngắn.

*Thao tác: Đi lần lượt qua bốn ví dụ bên trái.*

Ví dụ, dựng môi trường test hoặc staging tạm để chạy thử phiên bản mới. Dựng bản demo, PoC để trình bày và đánh giá. Chuẩn bị môi trường cho một buổi thực hành, workshop nhiều người. Hay chạy một tác vụ xử lý dữ liệu, tính toán ngắn hạn.

*Thao tác: Chỉ khung Đặc điểm chung bên phải.*

Các ví dụ này có ba đặc điểm chung. Thời gian sử dụng ngắn, từ vài giờ đến vài tuần. Nhu cầu lặp lại nhiều lần, mỗi lần cần một môi trường mới. Và người dùng chỉ cần chạy workload, không cần quản trị cluster.

Vấn đề là thời gian sử dụng ngắn nhưng công sức chuẩn bị môi trường không giảm theo, và lặp lại ở mỗi lần cần.

**Chuyển slide:** Cụ thể, việc chuẩn bị đó gồm những gì?

## Slide 06: Rào cản khi tiếp cận môi trường Kubernetes

*Thời gian gợi ý: 35 giây.*

*Thao tác: Đi theo sơ đồ từ nhu cầu sử dụng, qua phần chuẩn bị, tới bắt đầu sử dụng Kubernetes.*

Để có một môi trường dùng được, trước hết cần tài nguyên tính toán và lưu trữ phù hợp. Tiếp theo là thông tin đăng nhập và quyền thao tác. Môi trường cũng cần kết nối mạng cùng các thành phần hỗ trợ để workload hoạt động.

Những việc này có thể do người dùng tự làm hoặc do bên cung cấp hạ tầng chuẩn bị. Việc thiết lập và duy trì chúng đều đòi hỏi thời gian, kiến thức và công sức quản trị. Với nhu cầu ngắn hạn, phần chuẩn bị này lặp lại ở mỗi lần cần môi trường, trong khi thời gian sử dụng thực lại ngắn.

**Chuyển slide:** Từ đó, đồ án đặt ra bài toán cung cấp môi trường Kubernetes sẵn sàng sử dụng, giúp người dùng giảm công sức chuẩn bị hạ tầng.

## Slide 07: Mục tiêu đồ án

*Thời gian gợi ý: 30 giây.*

Đồ án hướng tới bốn mục tiêu. Người dùng có thể tự yêu cầu môi trường trên hạ tầng sẵn có. Môi trường được cấp với tài nguyên và quyền truy cập có thể cấu hình.

Người dùng tiếp tục sử dụng các công cụ Kubernetes tiêu chuẩn và tự lựa chọn workload. Còn việc cấp phát, cập nhật và dọn dẹp môi trường được quản lý qua một nền tảng tập trung.

**Chuyển slide:** Để lựa chọn cách thực hiện, em xem xét các hướng cung cấp môi trường Kubernetes hiện có.

## Slide 08: Dịch vụ Managed Kubernetes

*Thời gian gợi ý: 50 giây.*

Nhóm đầu tiên là Managed Kubernetes, với các ví dụ như Amazon EKS, Google GKE, Microsoft AKS và nhiều dịch vụ khác. Nhà cung cấp đảm nhiệm cài đặt và vận hành control plane, còn người dùng triển khai workload trên cluster.

Các dịch vụ này phù hợp khi cần khả năng mở rộng, tích hợp cloud, hoặc cần cluster có cấu hình và vòng đời độc lập.

Trong bài toán của đồ án, cần cân nhắc thêm hai tình huống. Nếu tạo cluster riêng cho từng nhu cầu nhỏ, tài nguyên có thể bị dư thừa, đồng thời phát sinh chi phí và công sức khởi tạo.

Nếu chọn chia sẻ cluster, bên tổ chức dịch vụ vẫn cần thiết lập cách cấp môi trường, hạn mức và quyền truy cập cho từng người dùng.

**Chuyển slide:** Ngoài các dịch vụ này, người dùng cũng có thể tự triển khai Kubernetes.

## Slide 09: Tự triển khai Kubernetes

*Thời gian gợi ý: 45 giây.*

*Thao tác: Trình bày lần lượt cột cluster tự quản lý và cột Kubernetes local.*

Với cluster tự quản lý, chẳng hạn dùng kubeadm, người vận hành chủ động lựa chọn hạ tầng và cấu hình Kubernetes. Đổi lại, họ phải chuẩn bị tài nguyên, cài đặt, nâng cấp và xử lý sự cố. Khi phục vụ nhiều người, họ còn cần tổ chức việc cấp môi trường và phân quyền.

Ở tình huống local, kind và minikube giúp tạo cluster trên máy cá nhân để học hoặc thử nghiệm. Tài nguyên và thời gian hoạt động khi đó phụ thuộc vào máy cá nhân. Muốn chia sẻ cho người khác cũng cần thiết lập thêm kết nối và quyền sử dụng.

**Chuyển slide:** Trước khi đi tiếp, em tổng kết hai cách cung cấp môi trường này trong một bảng so sánh. Kubernetes local là công cụ học và thử nghiệm trên máy cá nhân nên không đưa vào bảng.

## Slide 10: So sánh các cách có môi trường Kubernetes

*Thời gian gợi ý: 40 giây.*

*Thao tác: Đi theo từng hàng, dừng lại ở cột Kỳ vọng.*

Bảng này tổng kết hai cách vừa nêu. Cả hai hướng tới nhu cầu dài hạn, khi một cluster riêng được dùng lâu. Managed Kubernetes giải quyết phần vận hành control plane, nhưng mỗi yêu cầu vẫn là một cluster mới và tài nguyên tính theo node. Tự quản lý cho quyền chủ động nhưng dồn toàn bộ việc chuẩn bị và vận hành về người dùng. Khi nhiều người dùng chung một cluster, cả hai đều buộc bên tổ chức tự thiết lập thêm cơ chế cấp môi trường và quyền.

Cột cuối là điều đồ án kỳ vọng: phục vụ cả nhu cầu ngắn hạn và dài hạn, bên cung cấp vận hành cluster, môi trường gồm hạn mức tài nguyên theo yêu cầu trên cluster sẵn có, cơ chế cấp môi trường và quyền được tích hợp sẵn, và một nền tảng quản lý vòng đời.

**Chuyển slide:** Điểm chung của cột kỳ vọng là dùng chung một cluster. Vậy khi dùng chung, một môi trường bên trong được quản lý như thế nào?

## Slide 11: Chia sẻ cluster bằng namespace

*Thời gian gợi ý: 50 giây.*

Một cách phổ biến là dùng namespace để tổ chức tài nguyên thành các phạm vi bên trong cluster. Mỗi người dùng hoặc nhóm được cấp một hay nhiều namespace, kèm quyền truy cập, hạn mức và chính sách mạng.

*Thao tác: Chỉ nhóm A và nhóm B, mỗi nhóm có hai namespace.*

Khi một nhóm có nhiều namespace, hai vấn đề xuất hiện. Thứ nhất, Kubernetes không có sẵn một đơn vị để gom các namespace theo nhóm và quản lý chính sách chung cho cả nhóm đó. Các cơ chế quyền, quota và chính sách mạng vẫn có, nhưng cần được tổ chức nhất quán.

Thứ hai, nếu quản trị viên phải tạo và cấu hình thủ công từng namespace, số yêu cầu tăng sẽ khiến quản trị viên trở thành điểm nghẽn.

**Chuyển slide:** Capsule cung cấp cơ chế để tổ chức namespace theo nhóm và giảm phần việc cấp phát thủ công này.

## Slide 12: Capsule: Quản lý namespace theo Tenant

*Thời gian gợi ý: 50 giây.*

Capsule là một Kubernetes Operator, tức thành phần theo dõi cấu hình khai báo và tự động thực hiện các thao tác quản lý tương ứng.

Capsule bổ sung khái niệm Tenant. Một Tenant gom một hoặc nhiều namespace của người dùng hoặc nhóm, cùng quyền hạn, hạn mức và chính sách.

*Thao tác: Chỉ khung Tenant, sau đó đi qua ba bước bên trái.*

Quản trị viên cluster thiết lập Tenant và chỉ định người quản lý, gọi là Tenant Owner. Người này có thể tự tạo namespace trong phạm vi được cấp. Capsule áp dụng các cấu hình đã khai báo cho namespace thuộc Tenant, kể cả namespace mới.

**Chuyển slide:** Capsule giải quyết được hai vấn đề vừa nêu. Nhưng để trở thành một dịch vụ thì còn thiếu gì? Em so sánh trong bảng tiếp theo.

## Slide 13: So sánh cơ chế shared cluster

*Thời gian gợi ý: 50 giây.*

*Thao tác: Đi theo cột Capsule Tenant trước, rồi sang cột Kỳ vọng.*

Khi dùng chung cluster, một môi trường có thể là một hoặc nhiều namespace, hoặc một Capsule Tenant gom và quản lý các namespace. Cột Kỳ vọng giữ đúng đơn vị môi trường của Capsule.

Nhưng Capsule dừng ở Tenant. Không có đơn vị nào quản lý nhiều môi trường của một khách hàng. Quota đặt cho từng Tenant, quyền cho Tenant qua các group. Tenant do quản trị viên tạo, sửa, xóa. Cấp IPPool riêng không hỗ trợ sẵn, node chỉ là nodeSelector ghi vào Tenant, kubeconfig vẫn do quản trị viên chuẩn bị.

Cột Kỳ vọng bổ sung đúng những phần đó: tổ chức quản lý các môi trường, tài nguyên cấp cho tổ chức rồi chia cho các môi trường, phân quyền theo vai trò qua group của tổ chức, người dùng tự tạo, sửa, xóa môi trường dựa trên giới hạn trong tổ chức, tự chọn CIDR và IPPool có cơ chế chống trùng, chọn shared node hoặc dedicated node với tính toán remaining resource và auto label, kubeconfig cùng giao diện web cấp sẵn.

**Chuyển slide:** Capsule là cơ chế nền tốt. Phần cấp phát và gắn môi trường với tổ chức chính là lớp Tenantry xây dựng phía trên.

## Slide 14: Tenantry: Dịch vụ cung cấp môi trường Kubernetes

*Thời gian gợi ý: 55 giây.*

Capsule quản lý môi trường trong cluster khá tốt. Tenantry là lớp dịch vụ đứng trên Capsule, lo phần cấp phát và quản lý môi trường cho người dùng.

Mỗi môi trường trên Tenantry được gọi là một Workspace. Một Workspace gồm nhiều namespace, kèm hạn mức tài nguyên, vùng mạng, node được dùng và các nhóm người dùng cùng vai trò của họ. Trong Kubernetes, Workspace được hiện thực bằng một Capsule Tenant, nên Tenantry không thay thế Capsule mà dùng Capsule làm nền.

*Thao tác: Đi nhanh qua sáu khối bên dưới.*

Sáu năng lực này đúng bằng cột Kỳ vọng ở bảng vừa rồi. Tài nguyên cấp cho Organization rồi chia cho các Workspace. Người dùng tự tạo, sửa, xóa Workspace trong giới hạn được cấp. Mỗi Workspace có CIDR và IPPool riêng, có cơ chế chống trùng. Node chọn từ shared hoặc dedicated node của Organization, hệ thống tự tính remaining resource và auto label. Quyền gán cho Workspace theo vai trò, qua các group. Truy cập bằng kubeconfig hoặc giao diện web, cấp sẵn theo Workspace.

Người dùng chọn công việc cần chạy, bên cung cấp vận hành cluster bên dưới.

**Chuyển slide:** Quay lại bảng so sánh ở phần trước, lần này cột cuối là Tenantry.

## Slide 15: Yêu cầu hệ thống

*Thời gian gợi ý: 45 giây.*

*Thao tác: Đọc lướt cột trái, rồi cột phải.*

Từ mô tả dịch vụ, em rút ra các yêu cầu mà hệ thống phải đáp ứng.

Về chức năng: người dùng tự tạo, sửa, xóa Workspace qua Portal trong hạn mức được cấp; quản lý tổ chức gồm mời thành viên, tạo nhóm và gán vai trò; cấp phát tài nguyên phải bảo đảm tổng phân bổ không vượt hạn mức của tổ chức; cấp CIDR, IPPool riêng và chọn shared hoặc dedicated node; cấp kubeconfig và giao diện web theo danh tính người dùng.

Về phi chức năng: các Workspace phải cách ly về tài nguyên, quyền và mạng; trạng thái trên Portal phải khớp trạng thái thật trong cluster và thao tác lặp lại phải an toàn; người dùng vẫn dùng công cụ Kubernetes tiêu chuẩn; mô hình dữ liệu sẵn sàng cho nhiều cluster; và hệ thống ghi nhận trạng thái cấp phát cùng nhật ký thao tác.

Các yêu cầu này định hình kiến trúc và các cơ chế được trình bày ở phần sau.

**Chuyển slide:** Quay lại bảng so sánh ở phần trước, lần này cột cuối là Tenantry.

## Slide 16: Tenantry so với các cách hiện có

*Thời gian gợi ý: 35 giây.*

*Thao tác: Chỉ cột Tenantry, đối chiếu với cột Kỳ vọng ở slide 10.*

Tenantry phục vụ cả nhu cầu ngắn hạn và dài hạn với workload namespaced. Bên cung cấp dịch vụ vận hành cluster dùng chung. Workspace được cấp trên cluster sẵn có nên không phải tạo control plane mới. Mỗi Workspace nhận hạn mức tài nguyên theo yêu cầu, trên shared node hoặc dedicated node. Hệ thống cấp Workspace, quyền truy cập và phương thức truy cập qua kubeconfig hoặc giao diện web. Hệ thống theo dõi trạng thái, cập nhật và thu hồi Workspace.

Đây là mô tả thiết kế; phần thực nghiệm sẽ kiểm chứng. Đổi lại, Workspace không phải cluster riêng, nên workload cần tài nguyên cấp cluster nằm ngoài phạm vi.

**Chuyển slide:** Luồng sử dụng dịch vụ này được mô tả ở slide tiếp theo.

## Slide 17: Kiến trúc tổng thể Tenantry

*Thời gian gợi ý: 70 giây.*

Sơ đồ này gồm các thành phần của dịch vụ và quan hệ giữa chúng. Khung viền bao những thành phần chạy trong cluster Kubernetes.

*Thao tác: Đi theo hàng trên, từ trái sang phải.*

Người dùng thao tác trên Portal UI. Portal UI gọi Platform API qua HTTP. Platform API kiểm tra quyền và hạn mức rồi đọc ghi Database. Provisioning Worker đọc công việc từ Database, apply tài nguyên PlatformWorkspace lên Kubernetes API và nhận trạng thái trả về.

*Thao tác: Đi theo hàng dưới, từ phải sang trái.*

Workspace Controller theo dõi PlatformWorkspace và tạo Capsule Tenant, RBAC, NetworkPolicy và IPPool. Cả việc theo dõi lẫn việc tạo đều là lời gọi HTTP tới Kubernetes API, nên mũi tên có hai chiều. Kubernetes API là điểm tương tác chung nên được tô đậm. Khối Other Components phía dưới gom các thành phần còn lại của cluster, như etcd, kube-scheduler, kubelet, Capsule Operator và plugin mạng.

*Thao tác: Chỉ đường từ kubectl, Headlamp qua Capsule Proxy.*

Người dùng thao tác Kubernetes bằng kubectl hoặc Headlamp, đi qua Capsule Proxy tới cùng Kubernetes API. Tenantry không nằm trên đường này, nên nếu Portal dừng hoạt động thì người đã có kubeconfig vẫn làm việc được.

*Thao tác: Chỉ hàng chú thích ở dưới sơ đồ, rồi các mũi tên nét đứt màu xanh lục.*

Màu mũi tên cho biết giao thức: xanh dương liền là HTTP để gọi API và thao tác Kubernetes, nâu nét đứt là TCP đọc ghi cơ sở dữ liệu, xanh lục nét đứt là OIDC cho đăng nhập và xác thực token. Màu hộp phân biệt thành phần do đồ án xây dựng, màu xanh nhạt, với thành phần bên ngoài, màu trắng.

Các mũi tên nét đứt là quan hệ OIDC. Keycloak phát token khi đăng nhập. Platform API kiểm tra token và đồng bộ group. Capsule Proxy nhận bearer token của người dùng. Kubernetes API cũng được cấu hình OIDC và xác minh chữ ký token bằng khóa công khai của Keycloak, không gọi Keycloak cho từng yêu cầu.

**Chuyển slide:** Hai lớp này trao đổi với nhau qua một tài nguyên tùy chỉnh duy nhất.

## Slide 18: Custom Resource PlatformWorkspace

*Thời gian gợi ý: 60 giây.*

Slide này nói rõ hai lớp ở sơ đồ vừa rồi trao đổi với nhau bằng cách nào.

Kubernetes cho phép khai báo loại tài nguyên mới bằng CRD. Tenantry định nghĩa một loại tài nguyên thuộc nhóm tenantry.io, phiên bản v1alpha1, kind là PlatformWorkspace. Mỗi Workspace trên Portal tương ứng một đối tượng loại này trong cluster.

*Thao tác: Chỉ cột trái.*

Trong spec có hạn mức tài nguyên, danh sách namespace, chế độ compute kèm node được chọn, CIDR, vùng hostname và các group kèm vai trò. Phần status là trạng thái quan sát được do controller ghi lại.

*Thao tác: Chỉ bốn bước ở cột phải.*

Provisioning Worker apply CR này lên cluster. Workspace Controller theo dõi và tạo Capsule Tenant, RBAC, NetworkPolicy, IPPool và Certificate cho khớp với spec. Sau đó controller ghi status, worker đọc lại và Portal hiển thị trạng thái cho người dùng.

Điểm quan trọng là controller không chạy một lần rồi thôi. Nó liên tục đối chiếu trạng thái thật với spec và sửa phần lệch, nên nếu có tài nguyên bị xóa nhầm thì lần đối chiếu sau sẽ tạo lại.

Tóm lại, Portal ghi trạng thái mong muốn, controller đưa cluster về đúng trạng thái đó và báo lại qua status. Người dùng không thao tác trực tiếp với CR này; đây là contract nội bộ và cũng là chỗ để sau này thay cách vận chuyển mà không sửa phần còn lại.

**Chuyển slide:** Trước khi nói cách phân bổ tài nguyên, cần phân biệt hai chế độ compute mà một Workspace có thể chọn.

## Slide 19: Shared node và Dedicated node

*Thời gian gợi ý: 55 giây.*

*Thao tác: Đi theo từng hàng của bảng, cột Dedicated node ở bên phải.*

Khi tạo Workspace, người dùng chọn chế độ compute trong phạm vi Organization được cấp.

Với Shared node, Workspace chạy trên nhóm node dùng chung của nhiều Organization và chỉ nhận hạn mức trên nhóm node đó. Kubernetes đặt workload vào nhóm node được phép bằng nodeSelector. Chế độ này phù hợp với nhu cầu nhỏ và ngắn hạn.

Với Dedicated node, Organization được cấp riêng một node. Khi tạo Workspace, hệ thống giữ chỗ CPU, bộ nhớ và số Pod trên node đó, gắn label và taint riêng cho node, và Pod của Workspace mang toleration tương ứng. Chế độ này dành cho nhu cầu cần tài nguyên ổn định và tách biệt hơn.

Khác biệt bản chất nằm ở cách cấp: Shared là trần mềm nên có thể overcommit, còn Dedicated là giữ chỗ cứng trong capacity của node nên hệ số overcommit bằng 1. Cả hai chế độ vẫn dùng chung control plane, và một Dedicated node có thể phục vụ nhiều Workspace của cùng Organization.

**Chuyển slide:** Hai chế độ này được phân bổ trong phạm vi hạn mức mà Organization được cấp.

## Slide 20: Organization và phân bổ tài nguyên

*Thời gian gợi ý: 60 giây.*

Organization là đơn vị quản lý thành viên, group và các Workspace của một cá nhân hoặc tổ chức.

*Thao tác: Chỉ Platform Admin và mũi tên ClusterGrant.*

Platform Admin cấp cho mỗi Organization một ClusterGrant trên cluster: hạn mức CPU, bộ nhớ, lưu trữ và các lựa chọn hạ tầng được phép dùng.

*Thao tác: Chỉ ba ô số liệu.*

Trong ví dụ này, Organization được cấp 20 CPU và 40 GiB. Ba Workspace đang giữ 14 CPU và 28 GiB, còn lại 6 CPU và 12 GiB.

*Thao tác: Chỉ hai vùng con bên dưới.*

Cùng một hạn mức đó, nhưng ràng buộc khác nhau theo chế độ compute. Bên trái là shared pool: Workspace A lấy 4 CPU và 8 GiB làm trần mềm trên nhóm node dùng chung, có thể overcommit nên Pod phải chờ nếu pool hết chỗ. Bên phải là một dedicated node được cấp riêng cho Organization: Workspace B và Workspace C giữ chỗ cứng 6 và 4 CPU trên node đó, tổng giữ chỗ không bao giờ vượt capacity của node.

Org Owner cũng quyết định group nào được gán vào Workspace nào và với vai trò gì.

**Chuyển slide:** Khi một yêu cầu tạo Workspace được gửi đi, hệ thống xử lý theo các bước sau.

## Slide 21: Cấp phát và thu hồi Workspace

*Thời gian gợi ý: 55 giây.*

*Thao tác: Đi theo bốn bước tạo môi trường ở hàng trên.*

Đầu tiên, API kiểm tra quyền, hạn mức và tài nguyên khả dụng. Khi yêu cầu hợp lệ, hệ thống ghi nhận phần tài nguyên phân bổ cùng công việc cấp phát trong một giao dịch cơ sở dữ liệu.

Sau đó, Worker chuyển yêu cầu xuống cluster. Controller tạo Tenant và cấu hình môi trường. Khi quá trình này hoàn tất, hệ thống cập nhật trạng thái sẵn sàng và cung cấp thông tin truy cập.

*Thao tác: Chỉ luồng thu hồi ở hàng dưới.*

Khi xóa Workspace, hệ thống thu hồi quyền truy cập, dọn dẹp namespace, workload và dữ liệu liên quan theo chính sách xóa, rồi mới hoàn trả phần tài nguyên đã phân bổ. Nếu dọn dẹp chưa xong, phần phân bổ tương ứng vẫn cần được giữ lại để tránh cấp lại quá sớm.

**Chuyển slide:** Ngoài compute, mỗi Workspace còn có mạng, lưu trữ và đường truy cập ứng dụng riêng.

## Slide 22: Mạng, lưu trữ và truy cập ứng dụng

*Thời gian gợi ý: 55 giây.*

Về mạng, mỗi Workspace được cấp một IPPool riêng do Calico quản lý, và NetworkPolicy mặc định chặn lưu lượng giữa các Workspace. Chia dải địa chỉ và kiểm soát lưu lượng là hai cơ chế phối hợp với nhau.

Về lưu trữ, người dùng khai báo PVC, tức yêu cầu dung lượng cho workload, theo StorageClass được cấp và trong hạn mức dung lượng của Workspace.

*Thao tác: Đi từ User qua Ingress, Service tới Pod, rồi chỉ sang PVC.*

Để truy cập ứng dụng, người dùng khai báo Ingress theo IngressClass được cấp, với hostname nằm trong vùng tên miền riêng của Workspace; chứng chỉ TLS do cert-manager cấp. Yêu cầu HTTPS đi qua Ingress, tới Service rồi tới Pod chạy ứng dụng, và Pod gắn PVC khi cần lưu dữ liệu.

Hostname trên slide là ví dụ minh họa. Đây là đường truy cập ứng dụng, còn kubectl và giao diện web phục vụ thao tác quản lý tài nguyên.

**Chuyển slide:** Việc ai được truy cập và được làm gì được kiểm soát bằng cơ chế đăng nhập và phân quyền.

## Slide 23: Đăng nhập và phân quyền

*Thời gian gợi ý: 55 giây.*

Keycloak xác thực danh tính một lần cho cả Portal và các công cụ Kubernetes. Sau đó quyền được cấp ở hai phạm vi tách biệt.

*Thao tác: Chỉ bảng bên trái.*

Phạm vi thứ nhất là quyền trên Portal. Platform Admin là vai trò của bên vận hành dịch vụ: đăng ký cluster, tạo Organization cho khách hàng và cấp ClusterGrant. Ba vai trò còn lại thuộc về người dùng trong Organization. OrgOwner quản lý thành viên, group, Workspace và hạn mức. OrgAdmin làm được gần như vậy, trừ chuyển quyền sở hữu và xóa Organization. OrgMember chỉ xem Organization và các Workspace mình được cấp quyền.

Cả bốn vai trò này đều được Platform API kiểm tra trên cùng một token; riêng vai trò Platform Admin nằm ở realm role trong Keycloak.

*Thao tác: Chỉ bảng bên phải.*

Phạm vi thứ hai là quyền trong Workspace, gán cho group theo từng Workspace. TenantOwner tạo namespace và quản trị workload cùng RBAC trong Tenant. Developer quản lý workload trong namespace được phép. Viewer chỉ xem tài nguyên được cho phép và không đọc Secret.

Hai phạm vi này độc lập: một người có thể là OrgOwner trên Portal nhưng chỉ là Viewer trong một Workspace cụ thể.

**Chuyển slide:** Phần tiếp theo là thực nghiệm và kết quả.

## Slide 24: Thực nghiệm và Kết quả



## Slide 25: Kết luận và hướng phát triển

*Thời gian gợi ý: 40 giây.*

Đồ án đề xuất cung cấp môi trường Kubernetes dưới dạng Workspace trên hạ tầng dùng chung. Capsule quản lý các namespace theo Tenant, còn Tenantry tổ chức việc cấp môi trường, phân bổ tài nguyên và quản lý quyền sử dụng thành dịch vụ.

Người dùng lựa chọn workload trong phạm vi tài nguyên và quyền được cấp. Bên cung cấp chịu trách nhiệm vận hành cluster và các thành phần hạ tầng.

Hướng phát triển tiếp theo là hỗ trợ nhiều cluster, với thành phần cấp phát tại từng cluster kết nối tới nền tảng trung tâm.

**Chuyển slide:** Phần trình bày của em xin kết thúc tại đây.

## Slide 26: Cảm ơn

*Thời gian gợi ý: 10 giây.*

Em xin cảm ơn các thầy cô đã lắng nghe. Em mong nhận được các câu hỏi và góp ý của hội đồng về mô hình dịch vụ cũng như thiết kế của Tenantry.