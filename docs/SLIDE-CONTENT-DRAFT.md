# Tenantry: đề cương slide và bản thảo nội dung

**Phiên bản:** 6, 2026-09-14  
**Tài liệu nền:** [template.pdf](template.pdf), bài trình bày học thuật 21 trang, tỉ lệ 16:9  
**Đề cương đang dùng:** 26 slide, bao gồm một slide giữ chỗ cho phần Thực nghiệm và Kết quả (để trống)  
**Đối tượng người nghe:** Hội đồng bảo vệ đồ án. Phần mở đầu giả định người nghe chưa biết gì về tenancy trong Kubernetes.  
**Ngôn ngữ:** Tiếng Việt cho nội dung slide và ghi chú thuyết trình

## 1. Định hướng bài trình bày

Bài trình bày bắt đầu từ chính Kubernetes, sau đó giải thích vì sao người dùng cần một cách thuận tiện để tiếp cận môi trường Kubernetes. Người dùng có thể triển khai sản phẩm, học tập, thử nghiệm, hoặc dùng Kubernetes cho những mục đích khác trong phạm vi năng lực mà dịch vụ hỗ trợ. Tenantry cung cấp môi trường và quyền truy cập. Nó không áp đặt ứng dụng hay quy trình làm việc của người dùng.

Lập luận được phát triển theo trình tự sau:

1. Kubernetes mang lại những gì và người dùng cần truy cập nó vì những mục đích nào.
2. Nhu cầu được làm rõ nhất: sử dụng Kubernetes cho các tác vụ ngắn hạn, khi thời gian dùng ngắn nhưng công sức chuẩn bị môi trường không giảm theo. Dịch vụ vẫn phục vụ mọi nhu cầu truy cập Kubernetes trong phạm vi năng lực hỗ trợ.
3. Vì sao việc có được một môi trường Kubernetes dùng được vẫn có thể tốn nhiều công sức.
4. Các cách hiện có để có được môi trường đó, điểm mạnh và các đánh đổi của chúng, tổng kết bằng một bảng so sánh có cột Kỳ vọng.
5. Chia sẻ một cluster nghĩa là gì, giới thiệu namespace và tenant bằng ngôn ngữ dễ hiểu, rồi so sánh namespace thuần với Capsule để thấy phần còn thiếu của một dịch vụ.
6. Tenantry cung cấp dịch vụ gì, đối chiếu lại với bảng so sánh, người dùng nhận môi trường thế nào, rồi đến mô hình quản lý tài nguyên và cách hệ thống thực hiện việc cấp phát.
7. Thực nghiệm và Kết quả, phần nội dung tạm để trống.
8. Kết luận và hướng phát triển.

Sử dụng bảng màu xanh nước biển/trắng, tiêu đề cỡ lớn, nhãn section ở góc trên bên trái, và các trang kiến trúc lấy sơ đồ làm trung tâm. Có thể mở rộng phần thảo luận các giải pháp hiện có khi cần, thay vì ép số trang tương ứng một-một. Thông tin tác giả và logo đơn vị trong template vẫn để dạng placeholder cho tới khi có thông tin chính xác.

Chỉ phần **Nội dung trên slide** mới xuất hiện trên mặt slide. Các phần **Ghi chú thuyết trình**, **Định hướng hình ảnh** và **Nguồn** phục vụ việc rà soát và dựng bộ slide. Các phần về dịch vụ và kiến trúc mô tả hành vi dự kiến của Tenantry. Các tham chiếu tới branch hỗ trợ mô tả chi tiết cài đặt ở mức tổng quan, không biến bài trình bày thành một báo cáo tiến độ.

## 2. Tổng quan đề cương


| Slide | Phần                   | Tiêu đề                                    | Mục đích                                                                        |
| ----- | ---------------------- | ------------------------------------------ | ------------------------------------------------------------------------------- |
| 01    | Trang bìa              | Tenantry                                   | Giới thiệu dịch vụ và đề tài                                                    |
| 02    | Mục lục                | Nội dung trình bày                         | Giữ nguyên năm phần của template                                                |
| 03    | Giới thiệu             | Kubernetes                                 | Giải thích Kubernetes trước khi bàn về truy cập hay tenancy                     |
| 04    | Giới thiệu             | Nhu cầu truy cập môi trường Kubernetes     | Giới thiệu các nhu cầu chung của người dùng                                     |
| 05    | Giới thiệu             | Nhu cầu sử dụng Kubernetes ngắn hạn        | Làm rõ nhu cầu trọng tâm: ví dụ cụ thể và đặc điểm khiến việc chuẩn bị môi trường tốn kém |
| 06    | Giới thiệu             | Rào cản khi tiếp cận môi trường Kubernetes | Làm rõ công việc chuẩn bị môi trường trước khi người dùng có thể sử dụng        |
| 07    | Giới thiệu             | Mục tiêu đồ án                             | Xác định dịch vụ cần xây dựng                                                   |
| 08    | Các giải pháp hiện có  | Dịch vụ Managed Kubernetes                 | Giới thiệu nhóm dịch vụ và những điểm cần cân nhắc khi cấp nhiều cluster riêng  |
| 09    | Các giải pháp hiện có  | Tự triển khai Kubernetes                   | Phân biệt cluster tự quản lý và Kubernetes local, trường hợp phù hợp và hạn chế |
| 10    | Các giải pháp hiện có  | So sánh các cách có môi trường Kubernetes  | Tổng kết hai cách theo tiêu chí rút từ nhu cầu, rào cản và mục tiêu; cột cuối là Kỳ vọng |
| 11    | Các giải pháp hiện có  | Chia sẻ cluster bằng namespace             | Đặt bài toán phân chia cluster dùng chung và quản lý namespace theo nhóm        |
| 12    | Các giải pháp hiện có  | Capsule — Quản lý namespace theo Tenant               | Giải thích cơ chế đứng sau mô hình đã chọn                                      |
| 13 | Các giải pháp hiện có | So sánh cơ chế shared cluster | Namespace, Capsule Tenant và môi trường kỳ vọng: những gì cần có sẵn mà Capsule chưa có |
| 14 | Giải pháp đề xuất | Tenantry — Dịch vụ cung cấp môi trường Kubernetes | Giới thiệu Tenantry, Workspace và những gì người dùng nhận được |
| 15 | Giải pháp đề xuất | Yêu cầu hệ thống | Nêu các yêu cầu chức năng và phi chức năng mà dịch vụ phải đáp ứng |
| 16 | Giải pháp đề xuất | Tenantry so với các cách hiện có | Nhắc lại bảng slide 10, thay cột Kỳ vọng bằng Tenantry |
| 17 | Giải pháp đề xuất | Kiến trúc tổng thể Tenantry | Giải thích vai trò các thành phần, hướng tương tác và quan hệ xác thực |
| 18 | Giải pháp đề xuất | Custom Resource PlatformWorkspace | Giải thích CRD, spec, status và vòng lặp hội tụ |
| 19 | Giải pháp đề xuất | Shared node và Dedicated node | Phân biệt hai chế độ compute theo cách cấp tài nguyên và vị trí workload |
| 20 | Giải pháp đề xuất | Organization và phân bổ tài nguyên | Giải thích Organization, hạn mức và cách phân bổ cho Workspace |
| 21 | Giải pháp đề xuất | Cấp phát và thu hồi Workspace | Giải thích cấp phát, theo dõi trạng thái và hoàn trả tài nguyên |
| 22 | Giải pháp đề xuất | Mạng, lưu trữ và truy cập ứng dụng | Giải thích hạ tầng mạng, lưu trữ và đường truy cập ứng dụng |
| 23 | Giải pháp đề xuất | Đăng nhập và phân quyền | Giải thích đăng nhập và hai phạm vi phân quyền |
| 24 | Thực nghiệm và Kết quả | Thực nghiệm và Kết quả | Chỉ có tiêu đề phần; nội dung để trống |
| 25 | Kết luận | Kết luận và hướng phát triển | Kết luận về thiết kế dịch vụ, phạm vi và hướng mở rộng |
| 26 | Hỏi đáp | Cảm ơn | Cảm ơn và mời thảo luận |




## 3. Nội dung chi tiết từng slide



### Slide 01: Tenantry

**Phần:** Trang bìa  
**Tham chiếu template:** Trang 1, tiêu đề bên trái và hình minh họa bên phải

**Nội dung trên slide**

**Tenantry**

Kubernetes Tenant as a Service với Capsule

Truy cập tự phục vụ tới các môi trường Kubernetes

Đồ án tốt nghiệp đại học

[Tên tác giả]
[Mã sinh viên / lớp]
Giảng viên hướng dẫn: [Tên giảng viên]
[Đơn vị đào tạo]
[Ngày bảo vệ]

**Ghi chú thuyết trình**

Tenantry là một dịch vụ cung cấp cho người dùng các môi trường Kubernetes trên hạ tầng sẵn có. Dịch vụ trao cho họ quyền truy cập cùng một lượng tài nguyên được cấp phát, trong khi nền tảng đảm nhiệm việc vận hành cluster bên dưới. Các slide tiếp theo sẽ trình bày bài toán truy cập trước khi đi sâu vào mô hình tenant.

**Định hướng hình ảnh:** Bám theo bố cục tiêu đề bên trái của template. Dùng hình ảnh hạ tầng Kubernetes ở bên phải. Giữ trang bìa đơn giản, tránh liệt kê các thành phần kỹ thuật.

**Nguồn:** PRODUCT-PLAN §§1–2, 13.

### Slide 02: Nội dung trình bày

**Phần:** Mục lục  
**Tham chiếu template:** Trang 2, năm mục được đánh số

**Nội dung trên slide**

1. Giới thiệu
2. Các giải pháp hiện có
3. Giải pháp đề xuất
4. Thực nghiệm và Kết quả
5. Kết luận và hướng phát triển

**Ghi chú thuyết trình**

Bắt đầu từ Kubernetes và nhu cầu về hạ tầng dễ tiếp cận. So sánh các hướng tiếp cận hiện có, rồi trình bày mô hình dịch vụ và kiến trúc của Tenantry.

**Định hướng hình ảnh:** Giữ các dấu đánh số màu xanh nước biển và cấu trúc học thuật năm phần.

**Nguồn:** template.pdf tr.2.

### Slide 03: Kubernetes

**Phần:** Giới thiệu  
**Tham chiếu template:** Trang 3, tổng quan Kubernetes

**Nội dung trên slide**

- Nền tảng mã nguồn mở để quản lý các workload container hóa.
- Được Google mã nguồn mở lần đầu năm 2014.
- Tự động hóa việc triển khai, mở rộng quy mô và phục hồi sau sự cố.
- Cung cấp một API chung cho tài nguyên tính toán, mạng và lưu trữ.

**Một cluster bao gồm control plane cùng các worker node chạy workload.**

**Ghi chú thuyết trình**

Kubernetes điều phối workload trên một nhóm máy gọi là cluster. Control plane quản lý trạng thái mong muốn, còn các worker node chạy container trong Pod. Người dùng mô tả điều họ muốn thông qua Kubernetes API, và hệ thống sẽ duy trì trạng thái đó.

Phần này cung cấp đủ thuật ngữ cho slide kế tiếp: muốn dùng Kubernetes thì phải có quyền truy cập vào một cluster thực tế và đủ tài nguyên. Chưa cần giải thích tenant, Capsule hay Organization ở đây.

**Định hướng hình ảnh:** Bám sát trang 3 của template: tiêu đề và logo Kubernetes bên trái, hình minh họa cluster nhỏ bên phải. Thể hiện một control plane và các worker node. Dùng nhãn năng lực ngắn gọn thay vì sơ đồ thành phần chi tiết.

**Nguồn:** [Tổng quan Kubernetes](https://kubernetes.io/docs/concepts/overview/), [Các thành phần Kubernetes](https://kubernetes.io/docs/concepts/overview/components/).

### Slide 04: Nhu cầu truy cập môi trường Kubernetes

**Phần:** Giới thiệu  
**Tham chiếu template:** Bố cục diễn giải của trang 3, đặt sau phần tổng quan Kubernetes

**Nội dung trên slide**

Người dùng cần truy cập Kubernetes vì nhiều mục đích khác nhau:

- Triển khai và vận hành sản phẩm hoặc dịch vụ.
- Thực hiện các tác vụ ngắn hạn: xử lý dữ liệu, kiểm thử, chạy thử.
- Học Kubernetes và thử nghiệm với các API.

**Điểm chung: một môi trường dùng được, có tài nguyên và quyền truy cập.**

**Ghi chú thuyết trình**

Dịch vụ hữu ích cho cá nhân, nhóm và tổ chức. Những hoạt động kể trên chỉ là ví dụ về các cách sử dụng, không phải các tính năng riêng biệt của sản phẩm hay giới hạn về đối tượng được dùng Tenantry.

Dịch vụ phục vụ cả ba nhu cầu. Trong đó, nhu cầu thứ hai, các tác vụ ngắn hạn, được làm rõ ở slide tiếp theo vì đây là trường hợp mà cách tiếp cận hiện có đáp ứng kém thuận tiện nhất: triển khai lâu dài thường đã có cluster production và quy trình riêng, học tập thường được đáp ứng bằng cluster local, còn chạy một tác vụ ngắn trên hạ tầng thật thì chưa có cách gọn.

Bên cung cấp hạ tầng lo phần môi trường, quyền hạn và dung lượng tài nguyên. Người dùng tự quyết định chạy gì trong phạm vi năng lực Kubernetes được hỗ trợ. Sự phân định này giúp đồ án tập trung vào việc cấp phát hạ tầng, thay vì xây dựng một sản phẩm triển khai ứng dụng cho một nhóm đối tượng cụ thể.

**Định hướng hình ảnh:** Đặt một môi trường Kubernetes ở trung tâm, xung quanh là ba nhu cầu trong ba khung cùng kiểu (viền xanh, nền nhạt), căn tâm với khung trung tâm để mũi tên chạy thẳng. Biểu tượng: tên lửa, đồng hồ bấm giờ, mũ tốt nghiệp. Tránh trình bày lập trình viên ứng dụng như persona duy nhất, và chưa giới thiệu các đối tượng tenant ở đây.

**Nguồn:** PRODUCT-PLAN §1 (người dùng “muốn có tenant trong vài phút”, không muốn overhead của cluster riêng) và §2.4; cách đặt vấn đề theo use-case bám theo định hướng rà soát của tác giả. Các ví dụ mang tính minh họa, không phải kết quả khảo sát người dùng.

### Slide 05: Nhu cầu sử dụng Kubernetes ngắn hạn

**Phần:** Giới thiệu  
**Tham chiếu template:** Bố cục diễn giải của trang 3–4, một slide làm rõ nhu cầu trọng tâm trước khi nêu rào cản

**Nội dung trên slide**

Nhiều công việc chỉ cần môi trường Kubernetes trong thời gian ngắn.

**Ví dụ**

- **Kiểm thử ứng dụng:** môi trường test, staging tạm để chạy thử phiên bản mới.
- **Demo và PoC:** dựng bản thử nghiệm để trình bày và đánh giá trước khi áp dụng.
- **Thực hành và workshop:** môi trường cho nhiều người trong một buổi hoặc vài ngày.
- **Xử lý dữ liệu, tính toán:** chạy xử lý dữ liệu hoặc tính toán ngắn hạn.

**Đặc điểm chung**

- **Thời gian ngắn:** dùng từ vài giờ đến vài tuần.
- **Lặp lại nhiều lần:** mỗi lần cần một môi trường mới.
- **Chỉ cần chạy workload:** không cần quản trị cluster.

**Thời gian sử dụng ngắn, nhưng công sức chuẩn bị môi trường không giảm theo và lặp lại mỗi lần.**

**Ghi chú thuyết trình**

Slide này làm rõ nhu cầu trọng tâm đã đánh dấu ở slide 04. Điểm chung của các ví dụ là môi trường chỉ được dùng trong một khoảng thời gian ngắn, sau đó không còn cần nữa. Danh sách ví dụ mang tính minh họa và có thể mở rộng. Không đưa các trường hợp tạo môi trường tự động từ CI hay theo từng nhánh mã nguồn, vì chúng đòi hỏi tự động hóa qua API mà MVP chưa nhắm tới. Điều quan trọng là ba đặc điểm ở cột phải.

Ba đặc điểm này giải thích vì sao nhu cầu chưa được đáp ứng thuận tiện. Vì thời gian dùng ngắn và lặp lại nhiều lần, công sức chuẩn bị môi trường trở thành phần chi phí lớn so với thời gian sử dụng thực. Vì chỉ cần chạy workload trong namespace, một cluster riêng với control plane riêng là quá mức cần thiết. Khi môi trường không còn cần nữa, việc dọn dẹp và thu hồi tài nguyên cũng cần gọn; ý này được trình bày ở phần cấp phát và thu hồi Workspace.

Các ví dụ đều là workload namespaced trong phạm vi capability contract của Tenantry: Job, CronJob, Deployment, Service, Ingress, PVC. Không nêu các trường hợp cần CRD hay operator cluster-wide, vì chúng nằm ngoài phạm vi đồ án.

Slide tiếp theo liệt kê những gì phải chuẩn bị để có một môi trường dùng được; với nhu cầu ngắn hạn, phần chuẩn bị đó lặp lại ở mỗi lần cần môi trường.

**Định hướng hình ảnh:** Bên trái là lưới hai cột, hai hàng gồm bốn ví dụ, mỗi ví dụ có biểu tượng nét, tiêu đề ngắn và một dòng mô tả. Bên phải là khung nền nhạt “Đặc điểm chung” với ba mục. Câu kết ở cuối slide theo kiểu takeaway của template. Biểu tượng từ Lucide: flask-conical, presentation, users, database.

**Nguồn:** PRODUCT-PLAN §1 (nhu cầu “có tenant trong vài phút”, không cần control plane riêng, không cần quyền cluster-scoped) và §2.4 (capability contract: Job, CronJob, Deployment, Ingress, PVC). Các ví dụ và đặc điểm được tổng hợp để giải thích bài toán, không phải kết quả khảo sát người dùng.

### Slide 06: Rào cản khi tiếp cận môi trường Kubernetes

**Phần:** Giới thiệu  
**Tham chiếu template:** Trang 4, động lực và thách thức

**Nội dung trên slide**

Để bắt đầu sử dụng, người dùng cần một môi trường Kubernetes đã được chuẩn bị:

- **Tài nguyên:** có CPU, bộ nhớ và dung lượng lưu trữ phù hợp với nhu cầu.
- **Quyền truy cập:** có thông tin đăng nhập và quyền triển khai, quản lý workload.
- **Cấu hình môi trường:** có kết nối mạng và các thành phần hỗ trợ cần thiết.

Việc thiết lập và duy trì môi trường đòi hỏi thời gian, kiến thức và công sức quản trị.

**Bài toán: cung cấp môi trường Kubernetes sẵn sàng sử dụng, giúp người dùng giảm công sức chuẩn bị hạ tầng.**

**Ghi chú thuyết trình**

Hai slide trước trình bày những việc người dùng muốn làm với Kubernetes, trong đó trọng tâm là các tác vụ ngắn hạn. Slide này giải thích các điều kiện cần có để họ bắt đầu thực hiện những việc đó. Với nhu cầu ngắn hạn, các bước chuẩn bị này lặp lại ở mỗi lần cần môi trường, nên công sức chuẩn bị chiếm phần lớn so với thời gian sử dụng thực. Ví dụ, một người đã có chương trình xử lý dữ liệu vẫn cần nơi để chạy chương trình, tài nguyên phù hợp và quyền tạo workload trên Kubernetes.

Các công việc chuẩn bị có thể do người dùng tự thực hiện hoặc do bên cung cấp hạ tầng đảm nhiệm. Khi tự dựng, người dùng cần chuẩn bị tài nguyên, cài đặt và vận hành cluster. Khi dùng hạ tầng được cung cấp, họ cần được cấp tài nguyên, quyền truy cập và các cấu hình phù hợp. Khối lượng công việc phụ thuộc vào những gì môi trường đã có sẵn.

Điểm cần nhấn mạnh là công sức để có một môi trường dùng được. Một dịch vụ cung cấp môi trường có thể đảm nhiệm phần chuẩn bị hạ tầng, để người dùng tập trung vào công việc của mình. Đây là bước dẫn sang mục tiêu đồ án ở slide 07. Các lựa chọn cluster riêng hay dùng chung và đánh đổi của chúng được phân tích ở phần các giải pháp hiện có.

**Định hướng hình ảnh:** Minh họa một luồng từ “Nhu cầu sử dụng” qua khối “Chuẩn bị môi trường” đến “Bắt đầu sử dụng Kubernetes”. Trong khối chuẩn bị, đặt ba nhãn Tài nguyên, Quyền truy cập và Cấu hình môi trường. Ghi chú bên dưới khối này: “Thời gian · Kiến thức · Công sức quản trị”. Giữ bảng màu và kiểu tiêu đề của template.

**Nguồn:** PRODUCT-PLAN §§1–2; [Các thành phần Kubernetes](https://kubernetes.io/docs/concepts/overview/components/), [Multi-tenancy trong Kubernetes](https://kubernetes.io/docs/concepts/security/multi-tenancy/). Các điều kiện chuẩn bị được tổng hợp để giải thích bài toán tiếp cận môi trường; ví dụ mang tính minh họa.

### Slide 07: Mục tiêu đồ án

**Phần:** Giới thiệu  
**Tham chiếu template:** Trang 5, mục tiêu

**Nội dung trên slide**

- Cung cấp quyền truy cập Kubernetes theo hướng tự phục vụ trên hạ tầng sẵn có.
- Cấp phát môi trường với tài nguyên và quyền truy cập có thể cấu hình.
- Hỗ trợ các công cụ Kubernetes tiêu chuẩn và workload do người dùng tự chọn.
- Quản lý việc cấp phát, cập nhật và dọn dẹp thông qua một nền tảng tập trung.

**Mục tiêu dịch vụ: người dùng yêu cầu một môi trường và nhận được quyền truy cập mà không phải quản trị cluster bên dưới.**

**Ghi chú thuyết trình**

Mục tiêu là xây dựng một dịch vụ cung cấp môi trường Kubernetes. Dịch vụ đó cần đảm nhiệm phần việc cấp phát lặp đi lặp lại và quản lý vòng đời, đồng thời để người dùng tự quyết định làm gì bên trong môi trường được cấp.

Phần tiếp theo so sánh các cách để cung cấp dịch vụ này. Mô hình được chọn phải nêu rõ người dùng nhận được những năng lực Kubernetes nào và trách nhiệm quản trị nào vẫn thuộc về nhà cung cấp. Việc hạch toán tài nguyên chi tiết và các cơ chế cài đặt sẽ được trình bày sau khi lựa chọn đó đã rõ ràng.

**Định hướng hình ảnh:** Bốn mục tiêu ngắn gọn theo bố cục mục tiêu của template. Lấy “môi trường Kubernetes” làm trung tâm thay vì một đối tượng tenant hay Workspace chưa được giải thích.

**Nguồn:** PRODUCT-PLAN §§1–2, 22, 26.

### Slide 08: Dịch vụ Managed Kubernetes

**Phần:** Các giải pháp hiện có  
**Tham chiếu template:** Trang 6–7, các giải pháp hiện có và hạn chế

**Nội dung trên slide**

**Ví dụ: Amazon EKS, Google GKE, Microsoft AKS, …**

**Dịch vụ cung cấp**

- Cluster Kubernetes để người dùng triển khai và quản lý workload trên các worker node.
- Nhà cung cấp đảm nhiệm việc cài đặt và vận hành control plane.

**Phù hợp với**

- Hệ thống production cần khả năng mở rộng và tích hợp các dịch vụ cloud.
- Nhu cầu có cluster với cấu hình và vòng đời độc lập.

**Hạn chế**

- Cluster riêng có thể gây dư thừa tài nguyên và tăng chi phí overhead khi nhu cầu sử dụng nhỏ.
- Khởi tạo cluster mới làm phát sinh thời gian và công sức chuẩn bị.
- Khi chia sẻ cluster, cần tự thiết lập thêm cơ chế cấp môi trường, hạn mức tài nguyên và quyền truy cập cho từng người dùng.

**Ghi chú thuyết trình**

Managed Kubernetes là nhóm dịch vụ giúp người dùng có cluster Kubernetes mà nhà cung cấp đảm nhiệm phần vận hành control plane. EKS, GKE và AKS là ba ví dụ tiêu biểu; danh sách còn nhiều dịch vụ khác.

Các dịch vụ này phù hợp với cả hệ thống production và các nhu cầu khác, đặc biệt khi cần hạ tầng cloud, khả năng mở rộng hoặc cluster có cấu hình và vòng đời độc lập. Chúng giải quyết một phần đáng kể công việc chuẩn bị và vận hành đã nêu ở slide 06.

Ba hạn chế được xét trong bối cảnh người dùng cần một môi trường để sử dụng Kubernetes, nhưng không nhất thiết cần một cluster riêng. Hai ý đầu nói về việc cấp cluster riêng; ý cuối xét lựa chọn chia sẻ cluster.

**Tài nguyên và chi phí:** Khi mỗi nhu cầu nhỏ được cấp một cluster riêng, tài nguyên có thể bị chia nhỏ giữa các cluster và có phần chưa sử dụng hết. Ngoài tài nguyên chạy workload, một số dịch vụ còn có phí quản lý theo cluster, như EKS. Mức chi phí thực tế phụ thuộc cấu hình, chế độ dịch vụ và autoscaling. Tenantry chọn cấp nhiều môi trường trên hạ tầng dùng chung với hạn mức riêng, nhằm giảm việc phải duy trì cluster riêng cho từng nhu cầu. Đây là hướng thiết kế; mức tiết kiệm thực tế cần được đánh giá bằng thực nghiệm.

**Thời gian và công sức chuẩn bị:** Nếu bắt đầu bằng một cluster mới, cần khởi tạo cluster và hoàn tất những thiết lập phù hợp trước khi sử dụng. Tenantry cấp môi trường trên cluster đã được bên cung cấp chuẩn bị sẵn, nên mỗi yêu cầu không phải tạo một control plane mới. Việc cấp môi trường vẫn có thời gian xử lý và phụ thuộc tài nguyên sẵn có. Người dùng vẫn tự triển khai và cấu hình workload của mình.

**Cấp môi trường trên cluster dùng chung:** Managed Kubernetes hỗ trợ chia sẻ cluster. Để cung cấp cho từng người hoặc nhóm một môi trường có hạn mức và quyền truy cập phù hợp, bên tổ chức dịch vụ cần lựa chọn, cấu hình và kết nối các cơ chế cấp phát, quota, phân quyền và chính sách. “Cần tự thiết lập thêm” ở đây là phần việc của bên tổ chức dịch vụ; người nhận môi trường có thể được quản trị viên chuẩn bị giúp. Theo thiết kế, Tenantry tích hợp những việc này vào luồng tạo môi trường trên Portal và tự động áp dụng cấu hình tương ứng, trong phạm vi tài nguyên và chính sách đã được quản trị viên cho phép.

**Định hướng hình ảnh:** Đặt tên nhóm Managed Kubernetes cùng ba logo minh họa và dấu “…” ở đầu slide. Trình bày theo thứ tự Dịch vụ cung cấp → Phù hợp với → Hạn chế. Phần mô tả dịch vụ nằm ngay dưới tên, tiếp đến hai cột Phù hợp với và Hạn chế. Minh họa ba hạn chế theo đúng thứ tự: tài nguyên và chi phí, thời gian chuẩn bị, thiết lập môi trường dùng chung. Phân biệt hai trường hợp cluster riêng và cluster dùng chung.

**Nguồn:** [Tổng quan Amazon EKS](https://docs.aws.amazon.com/eks/latest/userguide/what-is-eks.html), [Tổng quan GKE](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/kubernetes-engine-overview), [Tổng quan AKS](https://learn.microsoft.com/en-us/azure/aks/what-is-aks). Phân tích cluster riêng và dùng chung dựa trên [GKE cluster multi-tenancy](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/multitenancy-overview) và [cách đặt vấn đề của Capsule](https://projectcapsule.dev/docs/overview/). Phần liên hệ với Tenantry dựa trên PRODUCT-PLAN §§1–2, 5, 17–19.

### Slide 09: Tự triển khai Kubernetes

**Phần:** Các giải pháp hiện có  
**Tham chiếu template:** Trang 7, một hướng tiếp cận hiện có khác

**Nội dung trên slide**

**Cluster tự quản lý — ví dụ: kubeadm**

Tự xây dựng cluster trên hạ tầng do mình quản lý.

**Phù hợp với**

- Nhu cầu chủ động cấu hình hạ tầng và các thành phần Kubernetes.

**Hạn chế**

- Phải tự chuẩn bị tài nguyên, cài đặt, nâng cấp và xử lý sự cố cluster.
- Khi phục vụ nhiều người, cần thiết lập thêm cơ chế cấp môi trường, quản lý tài nguyên và phân quyền.

**Kubernetes local — ví dụ: kind, minikube**

Tạo cluster ngay trên máy cá nhân để học tập và thử nghiệm.

**Phù hợp với**

- Học Kubernetes và thử nghiệm với các API.

**Hạn chế**

- Tài nguyên và thời gian hoạt động của môi trường phụ thuộc vào máy cá nhân.
- Muốn chia sẻ môi trường cho nhiều người cần thiết lập thêm truy cập từ xa và quyền sử dụng.

**Ghi chú thuyết trình**

Hai nhóm cùng là cách tự triển khai Kubernetes, nhưng được trình bày theo hai tình huống sử dụng: chủ động xây dựng và vận hành cluster trên hạ tầng của mình, hoặc tạo môi trường local để học và thử nghiệm. Đây là cách nhóm ví dụ để giải thích, không phải hai loại Kubernetes hoàn toàn tách biệt. kubeadm, kind và minikube là các công cụ hỗ trợ triển khai, không phải dịch vụ cung cấp sẵn tài nguyên cho người dùng.

**Cluster tự quản lý:** kubeadm hỗ trợ khởi tạo các thành phần của cluster. Người vận hành vẫn cần chuẩn bị máy, cài đặt các thành phần bổ sung như mạng cho Pod và quản lý quá trình vận hành về sau. Hướng này phù hợp khi cần quyền quản trị sâu hoặc muốn học cách xây dựng cluster. Nếu mục đích chỉ là sử dụng Kubernetes để chạy workload, phần việc đó làm tăng yêu cầu về kiến thức và công sức chuẩn bị. Khi tổ chức hạ tầng dùng chung, bên cung cấp còn cần thiết lập cách cấp môi trường, tài nguyên và quyền truy cập cho từng người hoặc nhóm.

**Kubernetes local:** kind và minikube giúp tạo cluster thuận tiện; minikube hướng tới học tập và phát triển, còn kind được thiết kế ban đầu để kiểm thử Kubernetes và cũng dùng cho phát triển hoặc CI. Trong tình huống chạy trên máy cá nhân được xét ở slide, workload dùng tài nguyên của máy đó và dừng hoạt động nếu máy hoặc môi trường chạy cluster ngừng hoạt động. Nếu muốn người khác cùng truy cập, cần chuẩn bị thêm kết nối và quyền phù hợp. Các công cụ này cũng có thể chạy trên máy chủ hoặc được tự động hóa; hạn chế ở đây gắn với cách sử dụng local trên máy cá nhân.

**Liên hệ với Tenantry:** Theo thiết kế, người dùng nhận môi trường trên hạ tầng đã được bên cung cấp chuẩn bị, cùng hạn mức tài nguyên và quyền truy cập. Họ giảm phần việc tự dựng cluster và có thể chạy workload trên hạ tầng tập trung thay vì phụ thuộc tài nguyên máy cá nhân. Quản trị viên của dịch vụ vẫn chịu trách nhiệm vận hành cluster bên dưới; môi trường vẫn chịu giới hạn tài nguyên và tình trạng hoạt động của hạ tầng đó. Tenantry phục vụ nhu cầu sử dụng Kubernetes trong phạm vi được cấp. Nếu cần học quản trị control plane hoặc tùy chỉnh toàn bộ cluster, tự triển khai vẫn là lựa chọn phù hợp hơn.

**Định hướng hình ảnh:** Hai cột: Cluster tự quản lý và Kubernetes local. Mỗi cột có ví dụ công cụ, một câu mô tả, rồi hai mục Phù hợp với và Hạn chế. Dùng hình máy chủ cho cột đầu, máy cá nhân cho cột sau. Căn ngang các mục tương ứng để người xem dễ so sánh.

**Nguồn:** [Tạo cluster với kubeadm](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/create-cluster-kubeadm/), [Tổng quan kind](https://kind.sigs.k8s.io/), [Bắt đầu với minikube](https://minikube.sigs.k8s.io/docs/start/). Phần phân tích cách sử dụng và quản lý dịch vụ là diễn giải kiến trúc; liên hệ với Tenantry dựa trên PRODUCT-PLAN §§1–2, 17–19.

### Slide 10: So sánh các cách có môi trường Kubernetes

**Phần:** Các giải pháp hiện có  
**Tham chiếu template:** Trang 7–8, tổng kết các giải pháp hiện có

**Nội dung trên slide**

| Tiêu chí | Managed Kubernetes | Cluster tự quản lý | Kỳ vọng |
| --- | --- | --- | --- |
| Nhu cầu sử dụng | Chủ yếu dài hạn | Chủ yếu dài hạn | Ngắn hạn và dài hạn |
| Vận hành cluster | Nhà cung cấp vận hành control plane | Người dùng tự cài đặt, nâng cấp, xử lý sự cố | Bên cung cấp dịch vụ đảm nhiệm |
| Yêu cầu môi trường mới | Khởi tạo cluster mới | Chuẩn bị máy và cài đặt cluster | Cấp trên cluster sẵn có |
| Tài nguyên cấp phát | Cả cluster, tính theo node | Cả cluster, trên máy tự chuẩn bị | Hạn mức tài nguyên theo yêu cầu, trên hạ tầng dùng chung |
| Cấp cluster cho nhiều người dùng | Tự thiết lập thêm cơ chế cấp môi trường và quyền | Tự thiết lập thêm cơ chế cấp môi trường và quyền | Tích hợp sẵn cơ chế cấp môi trường và quyền |
| Vòng đời môi trường | Người dùng tự quản lý theo từng cluster | Người dùng tự quản lý | Cấp phát, cập nhật, dọn dẹp qua một nền tảng |

**Thay vì cấp cả cluster, kỳ vọng là cấp môi trường theo hạn mức trên cluster sẵn có.**

**Ghi chú thuyết trình**

Slide tổng kết phần các giải pháp hiện có. Kubernetes local không đưa vào bảng vì đó là công cụ học và thử nghiệm trên máy cá nhân, không phải cách cung cấp môi trường cho người dùng. Sáu tiêu chí rút từ nhu cầu ở slide 04–05, ba điều kiện ở slide 06 và bốn mục tiêu ở slide 07. Cột Kỳ vọng nêu điều dịch vụ cần đạt mà chưa nhắc tên Tenantry; slide 16 quay lại đúng bảng này với cột Tenantry.

**Nhu cầu sử dụng:** Hai cách hiện có hướng tới nhu cầu dài hạn, khi một cluster riêng được dùng lâu và công sức chuẩn bị được bù lại. Managed Kubernetes phù hợp với production và cluster độc lập, tự quản lý phù hợp khi cần chủ động cấu hình hạ tầng; bảng chỉ ghi ngắn, phần giải thích đã nằm ở slide 08–09.

**Vận hành cluster và yêu cầu môi trường mới:** Managed Kubernetes giải quyết phần control plane, nhưng mỗi yêu cầu vẫn là một cluster mới. Tự quản lý dồn cả việc chuẩn bị và vận hành về người dùng. Kỳ vọng là bên cung cấp vận hành cluster và cấp môi trường trên cluster sẵn có.

**Tài nguyên cấp phát:** Cả hai cách cấp nguyên cluster, đơn vị tài nguyên là node. Kỳ vọng là môi trường gồm hạn mức tài nguyên đúng với yêu cầu, cấp trên hạ tầng dùng chung.

**Cấp cluster cho nhiều người dùng và vòng đời:** Khi nhiều người dùng chung một cluster, cả hai cách đều buộc bên tổ chức tự thiết lập cơ chế cấp môi trường và quyền. Kỳ vọng là cơ chế đó được tích hợp sẵn trong dịch vụ, và một nền tảng quản lý cấp phát, cập nhật, dọn dẹp.

Không so sánh chi phí hay thời gian bằng số vì chưa có thực nghiệm. Điểm chung của cột Kỳ vọng là dùng chung một cluster, dẫn tới câu hỏi ở slide 11: khi dùng chung thì phân chia và quản lý bên trong như thế nào.

**Định hướng hình ảnh:** Bảng native bốn cột, hàng tiêu đề nền xanh đậm. Cột Kỳ vọng tô nền xanh nhạt và chữ đậm để nổi bật. Không dùng biểu tượng đúng/sai. Câu kết luận ở dưới bảng.

**Nguồn:** Tổng hợp từ slide 08–09 và các nguồn tương ứng; tiêu chí theo slide 04–07; PRODUCT-PLAN §§1–2.

### Slide 11: Chia sẻ cluster bằng namespace

**Phần:** Các giải pháp hiện có  
**Tham chiếu template:** Trang 8, giới thiệu nền tảng kỹ thuật

**Nội dung trên slide**

**Cách tiếp cận**

- Cấp một hoặc nhiều namespace cho từng người dùng hoặc nhóm trên cluster dùng chung.
- Kết hợp quyền truy cập, quota và network policy để quản lý phạm vi sử dụng.

**Hạn chế**

- **Thiếu cơ chế quản lý namespace theo nhóm** — Kubernetes không có sẵn cơ chế gom namespace theo từng nhóm người dùng và áp dụng chính sách nhất quán cho cả nhóm namespace.
- **Bùng nổ namespace** — Quản trị viên cluster trở thành điểm nghẽn khi phải tạo và cấu hình thủ công từng namespace.

**Ghi chú thuyết trình**

Mở đầu: “Vậy nếu một tổ chức hoặc khách hàng quyết định dùng chung một cluster thay vì tạo nhiều cluster, họ sẽ phân chia môi trường cho những người dùng hoặc nhóm khác nhau như thế nào? Một cách phổ biến là phân chia thông qua namespace.” Sau đó trình bày hai ý trong phần Cách tiếp cận.

Slide 08–10 nói về cách có được và vận hành cluster, kết lại bằng bảng so sánh với cột Kỳ vọng. Tại slide này, tổ chức đã chọn dùng chung một cluster; vấn đề cần giải quyết là phân chia và quản lý các phần môi trường bên trong. Cluster đó có thể là Managed Kubernetes hoặc do tổ chức tự triển khai.

Ví dụ, nhóm A có namespace cho phát triển và kiểm thử, nhóm B có các namespace khác trên cùng cluster. Quản trị viên cấu hình quyền truy cập, quota và network policy cho các namespace đó. Khi một nhóm sở hữu nhiều namespace, nhu cầu quản lý theo nhóm bắt đầu xuất hiện.

**Thiếu cơ chế quản lý namespace theo nhóm:** Kubernetes không có sẵn một đối tượng cấp trên để gom các namespace theo nhóm và quản lý chính sách chung cho cả nhóm đó. Kubernetes vẫn có RBAC, quota và network policy; các chính sách có thể được cấu hình cho từng namespace. Phần còn thiếu trong cách quản lý namespace thuần túy là cơ chế quản lý cả nhóm namespace như một đơn vị, đồng thời duy trì chính sách nhất quán khi namespace được thêm hoặc thay đổi.

**Bùng nổ namespace:** Khi số người dùng và namespace tăng, việc quản trị viên trực tiếp tạo và cấu hình từng namespace trở thành công việc lặp lại. Người dùng phụ thuộc vào quản trị viên để nhận môi trường mới; quản trị viên phải bảo đảm mỗi namespace có đúng quyền, quota và chính sách. Đây là tình huống quản lý thủ công mà cơ chế tự phục vụ và tự động hóa có thể cải thiện.

Hai vấn đề trên dẫn sang nhu cầu tổ chức namespace theo nhóm và tự động hóa việc cấp phát. Slide tiếp theo giới thiệu Capsule cùng khái niệm Tenant để giải thích cách thực hiện.

**Định hướng hình ảnh:** Minh họa một cluster chung với các namespace của nhóm A và nhóm B. Dùng đường bao nét đứt để thể hiện nhu cầu gom namespace theo nhóm, kèm các cấu hình quyền, quota và network policy cần nhất quán. Bên cạnh, thể hiện các yêu cầu tạo namespace cùng đi tới quản trị viên để minh họa điểm nghẽn khi làm thủ công. Phần chữ chỉ gồm Cách tiếp cận và Hạn chế.

**Nguồn:** Hai mô tả hạn chế theo mục The Problem trong [Tổng quan Capsule](https://projectcapsule.dev/docs/overview/). Cơ chế và phạm vi chia sẻ cluster tham khảo [Multi-tenancy trong Kubernetes](https://kubernetes.io/docs/concepts/security/multi-tenancy/); PRODUCT-PLAN §§1, 4.4–4.5.

### Slide 12: Capsule — Quản lý namespace theo Tenant

**Phần:** Các giải pháp hiện có  
**Tham chiếu template:** Trang 10, nền tảng của hệ thống đề xuất

**Nội dung trên slide**

**Capsule** là một Kubernetes Operator hỗ trợ quản lý nhiều người dùng hoặc nhóm trên cluster dùng chung.

**Tenant:** đơn vị gom một hoặc nhiều namespace của người dùng hoặc nhóm, cùng quyền hạn, hạn mức tài nguyên và chính sách quản lý.

**Cách hoạt động**

1. **Cluster Admin** tạo Tenant, chỉ định người quản lý và cấu hình hạn mức, chính sách.
2. **Tenant Owner** tự tạo và quản lý namespace trong phạm vi Tenant được cấp.
3. **Capsule** tự động áp dụng các cấu hình được khai báo cho namespace thuộc Tenant, kể cả namespace mới.

**Quản lý namespace theo nhóm và giảm việc cấp phát thủ công của Cluster Admin.**

**Ghi chú thuyết trình**

Mở đầu: “Để quản lý các namespace theo nhóm và giảm việc quản trị viên phải tạo, cấu hình từng namespace, Capsule bổ sung một đơn vị quản lý gọi là Tenant.” Operator ở đây là thành phần theo dõi cấu hình khai báo trong Kubernetes và tự động thực hiện các thao tác quản lý tương ứng.

**Nối với vấn đề quản lý theo nhóm:** Tenant tạo quan hệ giữa các namespace và người dùng hoặc nhóm sở hữu chúng. Quyền hạn, ngân sách tài nguyên và chính sách được quản lý cho đơn vị này. Những cấu hình được khai báo để áp dụng cho các namespace sẽ được Capsule phân phối và duy trì, giúp giảm việc cấu hình lặp lại.

**Nối với vấn đề bùng nổ namespace:** Sau khi được cấp Tenant, Tenant Owner có thể tự tạo namespace trong giới hạn cho phép. Ví dụ, khi cần thêm namespace để kiểm thử, người quản lý Tenant tạo namespace đó và Capsule áp dụng các cấu hình tương ứng. Cluster Admin vẫn đặt hạn mức, chính sách và quản lý hạ tầng chung. Việc tự tạo namespace là năng lực của Capsule, giúp giảm yêu cầu cấp phát thủ công gửi tới quản trị viên.

“Capsule là một thành phần nền tảng quan trọng để xây dựng mô hình dịch vụ cung cấp môi trường Kubernetes mang tên Tenantry. Với dịch vụ này, người dùng có thể yêu cầu một môi trường và nhận tài nguyên, cấu hình cùng quyền truy cập để sử dụng.”

**Định hướng hình ảnh:** Một Tenant bao quanh các namespace, có nhãn chung về quyền hạn, hạn mức và chính sách. Minh họa luồng ba bước: Cluster Admin thiết lập Tenant → Tenant Owner tạo namespace → Capsule áp dụng cấu hình. Làm nổi bật namespace mới nhận cấu hình đã khai báo. Dùng sơ đồ này để nối trực tiếp với hai vấn đề ở slide 11.

**Nguồn:** [Tổng quan Capsule](https://projectcapsule.dev/docs/overview/), [Quản lý namespace trong Capsule](https://projectcapsule.dev/docs/tenants/namespaces/); PRODUCT-PLAN §§4.4, 13, 18.

### Slide 13: So sánh cơ chế shared cluster

**Phần:** Các giải pháp hiện có  
**Tham chiếu template:** Trang 10, phân tích nền tảng đã chọn

**Nội dung trên slide**

| Tiêu chí | Namespace | Capsule Tenant | Kỳ vọng |
| --- | --- | --- | --- |
| Đơn vị môi trường | Một hoặc nhiều namespace là một môi trường | Một Tenant gom và quản lý các namespace là một môi trường | Một đơn vị gom và quản lý các namespace, như Capsule Tenant |
| Đơn vị quản lý môi trường | Không có đơn vị quản lý các môi trường | Không có đơn vị quản lý các môi trường | Đơn vị Tổ chức quản lý các môi trường |
| Cấp phát tài nguyên | Quota cho từng namespace | Quota cho từng Tenant | Cấp cho tổ chức, chia cho các môi trường |
| Phân quyền | Cho từng namespace | Cho Tenant qua các group | Cho môi trường, theo vai trò, qua các group |
| Vòng đời môi trường | Quản trị viên tạo, sửa, xóa từng namespace | Quản trị viên tạo, sửa, xóa Tenant | Người dùng tự tạo, sửa, xóa môi trường dựa trên giới hạn trong tổ chức |
| IPPool riêng | Không hỗ trợ sẵn | Không hỗ trợ sẵn | Người dùng tự chọn CIDR và IPPool cho môi trường, có cơ chế chống trùng |
| Node cho môi trường | Quản trị viên đặt nodeSelector cho từng namespace | Đặt nodeSelector cho cả Tenant | Chọn shared node hoặc dedicated node của tổ chức, tự tính remaining resource, auto label các node |
| Truy cập | Kubeconfig do quản trị viên chuẩn bị | Kubeconfig do quản trị viên chuẩn bị, qua Capsule Proxy | Kubeconfig và giao diện web cấp sẵn |

**Capsule Tenant quản lý các namespace và gom nhóm thành một môi trường khá tốt, nhưng việc quản lý các môi trường, cấp tài nguyên, mạng, node và kết nối cho từng môi trường vẫn cần một lớp dịch vụ.**

**Ghi chú thuyết trình**

Slide so sánh ở phạm vi hẹp hơn bảng slide 10: cluster đã được dùng chung, câu hỏi là môi trường bên trong được định nghĩa, cấp và quản lý thế nào. Cột Namespace là cách ở slide 11, cột Capsule Tenant là cơ chế ở slide 12. Cột Kỳ vọng chính là Workspace, được đặt tên ở slide 14; các tiêu chí được chọn là những gì Workspace cần có sẵn mà Capsule Tenant không có.

**Đơn vị môi trường và đơn vị quản lý môi trường:** Với namespace, một hoặc nhiều namespace được coi là một môi trường. Capsule Tenant gom và quản lý các namespace thành một môi trường; kỳ vọng giữ đúng đơn vị này. Nhưng cả hai đều không có đơn vị nào đứng trên để quản lý nhiều môi trường của một khách hàng hoặc nhóm. Kỳ vọng là tổ chức quản lý các môi trường, tức Organization ở slide 19.

**Cấp tài nguyên và phân quyền:** Quota đặt cho từng namespace hoặc từng Tenant. Kỳ vọng là tài nguyên cấp cho tổ chức, các môi trường chia nhau trong phần đó. Phân quyền của Capsule cho Tenant qua các group là cách tốt; kỳ vọng giữ cách này nhưng group thuộc tổ chức và quyền theo vai trò Owner, Developer, Viewer.

**Vòng đời môi trường:** Namespace và Tenant đều do quản trị viên tạo, sửa, xóa; Tenant là tài nguyên cấp cluster nên người dùng không tự làm được. Kỳ vọng là người dùng tự tạo, sửa, xóa môi trường dựa trên giới hạn tài nguyên trong tổ chức, không cần quyền trên tài nguyên Tenant hay namespace của Kubernetes.

**IPPool riêng:** Namespace và Capsule không hỗ trợ sẵn việc cấp CIDR. Kỳ vọng là người dùng tự chọn CIDR và IPPool cho môi trường từ vùng địa chỉ của tổ chức, có cơ chế chống trùng.

**Node cho môi trường:** Capsule chỉ ghi nodeSelector vào Tenant; ai đó phải quyết định node nào và còn chỗ hay không. Kỳ vọng là chọn shared node hoặc dedicated node trong danh sách của tổ chức, tự tính remaining resource trên node và auto label, taint các node tương ứng.

**Truy cập:** Capsule Proxy lọc tài nguyên cấp cluster theo Tenant nhưng kubeconfig vẫn do quản trị viên chuẩn bị. Kỳ vọng là kubeconfig và giao diện web cấp sẵn theo môi trường.

Kết luận của slide: Capsule là cơ chế nền tốt. Phần bên trên, từ tổ chức, tự phục vụ, cấp mạng, gán node tới kubeconfig, là phần Tenantry xây dựng, được giới thiệu ở slide 14.

**Định hướng hình ảnh:** Bảng native bốn cột chiếm toàn bộ phần thân, câu dẫn “một môi trường được quản lý như thế nào” nói bằng lời. Cột Kỳ vọng tô nền xanh nhạt và chữ đậm. Ba cột dùng cùng khung câu, mỗi ô tối đa hai dòng. Câu kết luận ở dưới bảng.

**Nguồn:** [Tổng quan Capsule](https://projectcapsule.dev/docs/overview/), [Tenant trong Capsule](https://projectcapsule.dev/docs/tenants/), [Capsule Proxy](https://projectcapsule.dev/docs/proxy/), [Multi-tenancy trong Kubernetes](https://kubernetes.io/docs/concepts/security/multi-tenancy/); PRODUCT-PLAN §§1, 3, 4.2, 5–7, 10–11, 17.

### Slide 14: Tenantry — Dịch vụ cung cấp môi trường Kubernetes

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 10, giới thiệu hệ thống đề xuất

**Nội dung trên slide**

**Tenantry là lớp dịch vụ cấp và quản lý môi trường Kubernetes trên cluster dùng chung.**

Mỗi môi trường là một **Workspace**, quản lý nhiều namespace.

Workspace được hiện thực bằng một **Capsule Tenant**, kèm các cơ chế cấp hạn mức tài nguyên, mạng, node và quyền truy cập riêng.

- **Organization & quota:** Tài nguyên cấp cho Organization, chia cho các Workspace.
- **Self-service lifecycle:** Người dùng tự tạo, sửa, xóa Workspace trong giới hạn được cấp.
- **Network & IPPool:** Mỗi Workspace một CIDR và IPPool riêng, có cơ chế chống trùng.
- **Shared & dedicated node:** Chọn node của Organization, tự tính remaining resource và auto label.
- **Role-based access:** Gán quyền cho Workspace theo vai trò, qua các group.
- **kubeconfig & Web UI:** kubeconfig và giao diện web cấp sẵn theo Workspace.

**Người dùng chọn công việc cần chạy, bên cung cấp vận hành cluster bên dưới.**

**Ghi chú thuyết trình**

Slide trả lời trực tiếp câu kết ở slide 13: lớp dịch vụ còn thiếu chính là Tenantry. Mở đầu: “Capsule quản lý môi trường trong cluster khá tốt. Tenantry là lớp dịch vụ đứng trên Capsule, lo phần cấp phát và quản lý môi trường cho người dùng.”

**Định nghĩa Workspace:** Workspace là tên môi trường trên Tenantry, quản lý nhiều namespace. Trong Kubernetes, Workspace được hiện thực bằng một Capsule Tenant; phần “kèm theo” là các cơ chế Tenantry bổ sung quanh Tenant đó: cấp hạn mức tài nguyên lấy từ hạn mức của Organization, cấp CIDR và IPPool riêng, chọn node và tính phần còn lại, gán quyền theo vai trò cho các group, cấp kubeconfig và giao diện web. Vì vậy Tenantry không thay thế Capsule mà dùng Capsule làm nền.

**Sáu năng lực:** Đây đúng là sáu ô ở cột Kỳ vọng của bảng slide 13, nên có thể nhắc lại nhanh mà không cần giải thích lại từ đầu. Organization là đơn vị quản lý thành viên và hạn mức, được trình bày kỹ ở slide 19. Vòng đời tự phục vụ nghĩa là người dùng thao tác qua Portal, không cần quyền trên tài nguyên Tenant hay namespace của Kubernetes; các bước cấp phát ở slide 20. Mạng riêng nói kỹ ở slide 22, node ở slide 21, truy cập ở slide 23. kubeconfig là tệp cấu hình giúp công cụ như kubectl kết nối tới Kubernetes bằng danh tính của người dùng.

**Phạm vi:** Tenantry phục vụ cá nhân, nhóm và tổ chức, với workload nằm trong namespace. Các workload cần quyền quản trị node, cài đặt thành phần ở cấp cluster hoặc thêm loại tài nguyên Kubernetes mới nằm ngoài phạm vi Workspace.

**Định hướng hình ảnh:** Nửa trên là phần định nghĩa chạy hết chiều ngang, câu đầu in đậm màu xanh đậm. Nửa dưới là lưới ba cột hai hàng gồm sáu khối năng lực, mỗi khối một tiêu đề đậm và một câu mô tả. Giữ mỗi mô tả trong hai dòng. Slide này không dùng sơ đồ; phần luồng và sơ đồ để dành cho các slide sau.

**Nguồn:** PRODUCT-PLAN §§1–2, 2.4, 4.2, 4.4, 5–7, 10–11, 22; sáu năng lực lấy theo cột Kỳ vọng ở slide 13.

### Slide 15: Yêu cầu hệ thống

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 11, yêu cầu của hệ thống đề xuất

**Nội dung trên slide**

Hệ thống cần đáp ứng các yêu cầu chính sau:

**Yêu cầu chức năng**

- **Tự phục vụ:** Tạo, sửa, xóa Workspace qua Portal trong hạn mức được cấp.
- **Quản lý tổ chức:** Mời thành viên, tạo nhóm và gán vai trò trong Organization.
- **Cấp phát tài nguyên:** Chia hạn mức tổ chức cho từng Workspace, không vượt tổng.
- **Mạng và node:** Cấp CIDR, IPPool riêng và chọn shared hoặc dedicated node.
- **Truy cập:** Cấp kubeconfig và giao diện web theo danh tính người dùng.

**Yêu cầu phi chức năng**

- **Cách ly:** Workspace tách biệt về tài nguyên, quyền và mạng.
- **Nhất quán:** Trạng thái trên Portal khớp trạng thái thật trong cluster.
- **Tương thích:** Dùng công cụ Kubernetes tiêu chuẩn cho workload của người dùng.
- **Mở rộng:** Mô hình dữ liệu sẵn sàng cho nhiều cluster.
- **Theo dõi:** Ghi nhận trạng thái cấp phát và nhật ký thao tác.

**Các yêu cầu này định hình kiến trúc và các cơ chế được trình bày ở phần sau.**

**Ghi chú thuyết trình**

Slide chuyển từ mô tả dịch vụ sang các yêu cầu mà hệ thống phải đáp ứng. Đây là căn cứ để đánh giá thiết kế ở phần kiến trúc và là tiêu chí cho phần thực nghiệm.

**Yêu cầu chức năng** là những việc người dùng phải làm được. Tự phục vụ nghĩa là mọi thao tác vòng đời diễn ra trên Portal, không cần quyền trên tài nguyên cấp cluster. Quản lý tổ chức gồm mời thành viên, tạo nhóm và gán vai trò. Cấp phát tài nguyên phải giữ ràng buộc tổng phân bổ của các Workspace không vượt hạn mức Organization được cấp trên cluster đó. Mạng và node gồm cấp CIDR, IPPool riêng và chọn chế độ shared hoặc dedicated. Truy cập gồm kubeconfig theo danh tính người dùng và giao diện web.

**Yêu cầu phi chức năng** là các tính chất hệ thống phải giữ. Cách ly: một Workspace không dùng được tài nguyên, quyền hay mạng của Workspace khác. Nhất quán: trạng thái hiển thị trên Portal phải khớp trạng thái thật trong cluster, và một thao tác chạy lại nhiều lần cho cùng kết quả. Tương thích: người dùng dùng kubectl, Helm và các công cụ Kubernetes tiêu chuẩn. Mở rộng: mô hình dữ liệu gắn cluster cho mọi thực thể nên thêm cluster mới không phải sửa thiết kế. Theo dõi: trạng thái cấp phát và nhật ký thao tác phục vụ vận hành và phần đánh giá.

Nếu hội đồng hỏi về ưu tiên, nói rõ cách ly và nhất quán là hai yêu cầu bắt buộc; mở rộng nhiều cluster đã có chỗ trong thiết kế nhưng bản MVP chạy trên một cluster.

**Định hướng hình ảnh:** Một câu dẫn ở trên, bên dưới chia hai cột: Yêu cầu chức năng và Yêu cầu phi chức năng, mỗi cột năm mục gồm nhãn đậm và một câu mô tả. Có đường kẻ ngang dưới tiêu đề mỗi cột. Không dùng sơ đồ.

**Nguồn:** PRODUCT-PLAN §§1–2, 2.4, 5.3, 6–7, 10–11, 14, 17; architecture/02-data-model.

### Slide 16: Tenantry so với các cách hiện có

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 10–11, đối chiếu hệ thống đề xuất với các giải pháp hiện có

**Nội dung trên slide**

| Tiêu chí | Managed Kubernetes | Cluster tự quản lý | Tenantry |
| --- | --- | --- | --- |
| Nhu cầu sử dụng | Chủ yếu dài hạn | Chủ yếu dài hạn | Ngắn hạn và dài hạn |
| Vận hành cluster | Nhà cung cấp vận hành control plane | Người dùng tự cài đặt, nâng cấp, xử lý sự cố | Bên cung cấp dịch vụ vận hành cluster dùng chung |
| Yêu cầu môi trường mới | Khởi tạo cluster mới | Chuẩn bị máy và cài đặt cluster | Cấp Workspace trên cluster sẵn có |
| Tài nguyên cấp phát | Cả cluster, tính theo node | Cả cluster, trên máy tự chuẩn bị | Hạn mức tài nguyên theo yêu cầu cho từng Workspace, trên shared node hoặc dedicated node |
| Cấp cluster cho nhiều người dùng | Tự thiết lập thêm cơ chế cấp môi trường và quyền | Tự thiết lập thêm cơ chế cấp môi trường và quyền | Hệ thống cấp Workspace, quyền truy cập và phương thức truy cập qua kubeconfig hoặc giao diện web |
| Vòng đời môi trường | Người dùng tự quản lý theo từng cluster | Người dùng tự quản lý | Hệ thống cấp phát, theo dõi trạng thái và thu hồi Workspace |

**Tenantry cấp Workspace theo hạn mức trên cluster sẵn có, đúng với cột Kỳ vọng ở phần trước.**

**Ghi chú thuyết trình**

Slide nhắc lại bảng ở slide 10 với cột cuối là Tenantry, để người nghe đối chiếu ngay sau khi dịch vụ được giới thiệu. Hai cột giải pháp giữ nguyên nội dung. Đi theo cột Tenantry: phục vụ cả nhu cầu ngắn hạn và dài hạn, trong phạm vi workload namespaced tức các tài nguyên nằm trong namespace; bên cung cấp vận hành cluster dùng chung; Workspace được cấp trên cluster sẵn có nên không tạo control plane mới; mỗi Workspace nhận hạn mức tài nguyên theo yêu cầu trên shared node hoặc dedicated node; hệ thống cấp Workspace, quyền truy cập và phương thức truy cập qua kubeconfig hoặc giao diện web; Hệ thống theo dõi trạng thái, cập nhật và thu hồi Workspace.

Đây là mô tả thiết kế, chưa phải kết quả đo. Đánh đổi cần nói rõ: Workspace không phải cluster riêng, nên workload cần CRD hay tài nguyên cấp cluster nằm ngoài phạm vi (PRODUCT-PLAN §2.4). Với nhu cầu cần cluster độc lập hoặc học quản trị control plane, hai cách ở cột đầu vẫn phù hợp hơn.

**Định hướng hình ảnh:** Cùng bố cục bảng với slide 10, cột Tenantry thay cột Kỳ vọng và được tô nổi bật giống nhau để người xem nhận ra sự đối chiếu.

**Nguồn:** PRODUCT-PLAN §§1–2, 2.4, 5–6, 11, 17; tiêu chí và hai cột đầu theo slide 10.

### Slide 17: Kiến trúc tổng thể Tenantry

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 11, sơ đồ kiến trúc tổng thể

**Nội dung trên slide**

Một sơ đồ thành phần. Khung viền bao các thành phần chạy trong **Cluster Kubernetes**; các thành phần còn lại nằm ngoài khung.

**Thành phần**

- User · kubectl, Headlamp · Keycloak
- Portal UI · Platform API · Database · Provisioning Worker
- Trong cluster: Capsule Proxy · Kubernetes API · Workspace Controller · Other Components *(etcd, kube-scheduler, kubelet, Capsule Operator, Calico hoặc Cilium, …)*

**Mũi tên phân biệt bằng màu, chú thích một hàng ở dưới sơ đồ**

- Xanh dương liền — **HTTP: gọi API và thao tác Kubernetes.** Gồm User → Portal UI → Platform API, Provisioning Worker ↔ Kubernetes API, Workspace Controller ↔ Kubernetes API, kubectl và Headlamp → Capsule Proxy → Kubernetes API, Kubernetes API ↔ Other Components.
- Nâu nét đứt — **TCP: đọc ghi cơ sở dữ liệu.** Gồm Platform API ↔ Database và Database ↔ Provisioning Worker.
- Xanh lục nét đứt — **OIDC: đăng nhập và xác thực token.** Gồm Keycloak ↔ kubectl và Headlamp, Keycloak → Platform API, Keycloak → Capsule Proxy.

**Quy ước màu hộp, chỉ ba màu:** trắng là thành phần ngoài phạm vi đồ án, gồm User, Keycloak, kubectl và Headlamp, Capsule Proxy, Other Components; xanh nhạt là thành phần do đồ án xây dựng, gồm Portal UI, Platform API, Database, Provisioning Worker, Workspace Controller; xanh đậm là Kubernetes API, điểm tương tác chung.

**Ghi chú thuyết trình**

Sơ đồ có một khung viền duy nhất, bao các thành phần chạy trong cluster Kubernetes. Những thành phần còn lại là phần dịch vụ chạy ngoài cluster cùng người dùng và Keycloak. Cả Provisioning Worker lẫn Workspace Controller đều do đồ án viết, nên không tách khung riêng cho Tenantry.

**Đường quản lý:** Người dùng thao tác trên Portal UI. Portal UI gọi Platform API qua HTTP. Platform API kiểm tra quyền, hạn mức rồi đọc ghi Database. Database vừa lưu trạng thái dịch vụ vừa là hàng đợi công việc theo mô hình outbox. Provisioning Worker đọc công việc từ đó, apply tài nguyên PlatformWorkspace lên Kubernetes API và nhận trạng thái trả về, nên mũi tên giữa hai bên có hai chiều.

**Trong cluster:** Workspace Controller theo dõi PlatformWorkspace bằng cơ chế watch và tạo Capsule Tenant, RBAC, NetworkPolicy, IPPool và Certificate. Cả watch lẫn apply đều là lời gọi HTTP tới Kubernetes API, vì vậy mũi tên hai chiều và chỉ ghi một nhãn giao thức. Capsule và Calico nhận các tài nguyên đó rồi áp chính sách cho namespace của Workspace; phần này không vẽ trên sơ đồ để giữ sơ đồ gọn.

**Đường truy cập:** Người dùng dùng kubectl hoặc Headlamp đi qua Capsule Proxy tới cùng Kubernetes API. Tenantry không nằm trên đường này: nếu Portal dừng hoạt động, người đã có kubeconfig vẫn làm việc được.

**Quan hệ OIDC:** Mũi tên nét đứt màu xanh lục nối Keycloak với công cụ truy cập, Platform API và Capsule Proxy. Keycloak phát token khi đăng nhập; Platform API kiểm tra token và đồng bộ group nên mũi tên hai chiều; Capsule Proxy nhận bearer token của người dùng nên mũi tên một chiều. Kubernetes API cũng được cấu hình OIDC và xác minh chữ ký token bằng khóa công khai của Keycloak, không gọi Keycloak cho từng yêu cầu; quan hệ này không vẽ để sơ đồ khỏi có đường cắt nhau.

**Nếu được hỏi thêm:** Khối Other Components gom những thành phần của cluster không thuộc phạm vi đồ án nhưng cần cho cluster chạy. Phần lớn trong số đó nói chuyện với Kubernetes API qua HTTP; riêng etcd dùng gRPC. Ingress, lưu trữ và giám sát để dành cho slide 22. Tên công nghệ của từng thành phần nêu trong báo cáo, không đặt trên slide.

**Định hướng hình ảnh:** Một khung viền bao các thành phần trong cluster, nhãn CLUSTER KUBERNETES đặt ở góc trên bên phải khung. Kubernetes API đặt chính giữa khung, Other Components trải hết bề ngang khung ở hàng dưới. Các thành phần ngoài khung xếp cùng lưới cột để mũi tên chạy thẳng. Cột trái đặt người dùng, Keycloak và công cụ truy cập. Kubernetes API tô nền xanh đậm vì là điểm tương tác chung. Không ghi nhãn trên từng mũi tên vì phần lớn đều là HTTP; thay vào đó phân biệt bằng màu và kiểu nét, rồi đặt một hàng chú thích HTTP, TCP, OIDC ngay dưới khung, căn lề phải; mỗi mục chú thích có một câu mô tả ngắn. Quan hệ hai chiều dùng một mũi tên hai đầu. Mũi tên nét đứt màu xanh lục dành riêng cho OIDC, đi theo đường vuông góc và không cắt mũi tên nào; chú thích đặt ngoài khung ở góc dưới bên phải.

**Nguồn:** PRODUCT-PLAN §§13–14, 18; architecture/01-system-context §§1–2; nhánh scaffold `tenantry/README.md`, ADR-0102 và ADR-0104.

### Slide 18: Custom Resource PlatformWorkspace

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 12, mô hình dữ liệu và cơ chế xử lý

**Nội dung trên slide**

Mọi thay đổi Tenantry ghi vào cluster đi qua một Custom Resource mô tả trạng thái mong muốn của Workspace.

`apiVersion: tenantry.io/v1alpha1` · `kind: PlatformWorkspace`

| Trường | Nội dung |
| --- | --- |
| spec.organizationId | workspaceId, clusterId — định danh môi trường |
| spec.resources | Hạn mức CPU, bộ nhớ, Pod và dung lượng |
| spec.namespaces | Danh sách namespace mong muốn |
| spec.compute | Shared hoặc Dedicated, kèm node được chọn |
| spec.network | CIDR và IPPool của Workspace |
| spec.ingress | Vùng hostname và IngressClass được phép |
| spec.accessBindings | Group kèm vai trò TenantOwner, Developer, Viewer |
| status | phase, observedGeneration và tài nguyên đã tạo |

**RECONCILE LOOP**

1. **Provisioning Worker apply CR** — trạng thái mong muốn từ cơ sở dữ liệu
2. **Workspace Controller watch** — nhận thay đổi qua Kubernetes API
3. **Tạo và cập nhật tài nguyên** — Capsule Tenant, RBAC, NetworkPolicy, IPPool, Certificate
4. **Ghi status** — Worker đọc lại, Portal hiển thị trạng thái

**Portal ghi trạng thái mong muốn, controller đưa cluster về đúng trạng thái đó và báo lại qua status.**

**Ghi chú thuyết trình**

Slide này giải thích cách hai lớp ở sơ đồ kiến trúc nói chuyện với nhau. CRD, tức Custom Resource Definition, là cơ chế của Kubernetes cho phép khai báo một loại tài nguyên mới và quản lý nó bằng chính API của Kubernetes. Tenantry định nghĩa một loại tài nguyên tên PlatformWorkspace.

**Vì sao chọn cách này:** Thay vì để control plane gọi thẳng nhiều API khác nhau trong cluster, toàn bộ trạng thái mong muốn của một Workspace được gói vào một tài nguyên duy nhất. Nhờ đó chỉ có một cửa ghi vào cluster, dễ kiểm soát quyền của service account, dễ kiểm thử và dễ thay thế cách vận chuyển ở giai đoạn sau. Trong thiết kế Phase 2, một Cluster Agent có thể tự kéo CR này về thay cho việc worker apply trực tiếp, mà không phải sửa phần còn lại.

**Spec và status:** Ba trường định danh organizationId, workspaceId và clusterId nối đối tượng này với bản ghi trong cơ sở dữ liệu; metadata.name có dạng `<org-slug>-<ws-slug>` và cũng chính là tên Capsule Tenant. spec là trạng thái mong muốn do Portal quyết định; status là trạng thái quan sát được do controller ghi lại, gồm phase, observedGeneration, conditions và tham chiếu tới các tài nguyên đã tạo. Trường observedGeneration cho biết controller đã xử lý tới phiên bản nào của spec, nhờ đó Portal phân biệt được "đã nhận yêu cầu" và "đã hoàn tất".

**Vòng lặp hội tụ:** Controller không chạy một lần rồi thôi. Nó theo dõi CR, so trạng thái thật trong cluster với spec, và sửa phần lệch. Nếu có người xóa nhầm một RBAC hoặc NetworkPolicy, lần đối chiếu sau sẽ tạo lại. Thao tác lặp lại vì vậy an toàn, đúng yêu cầu nhất quán ở slide 15.

**Phạm vi:** Người dùng không có quyền trên CR này và cũng không cần biết nó tồn tại. Họ chỉ thấy Workspace trên Portal và các namespace trong cluster; CR là contract nội bộ giữa control plane và cluster.

**Định hướng hình ảnh:** Chia hai cột. Cột trái lấy chính dòng apiVersion và kind làm nhãn, ngay dưới là khung nền nhạt liệt kê các trường chính của spec và status với tên trường in đậm. Cột phải ghi nhãn RECONCILE LOOP rồi bốn thẻ có số thứ tự, tiêu đề và một dòng mô tả. Hai cột bắt đầu và kết thúc cùng một đường ngang. Không vẽ mũi tên vòng để tránh rối; ý lặp lại nói bằng lời.

**Nguồn:** PRODUCT-PLAN §§14.1–14.4, 17; architecture/01-system-context §2, 04-state-machines.

### Slide 19: Shared node và Dedicated node

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 13, so sánh các cấu hình của hệ thống

**Nội dung trên slide**

Khi tạo Workspace, người dùng chọn chế độ compute trong phạm vi Organization được cấp.

| Tiêu chí | Shared node | Dedicated node |
| --- | --- | --- |
| Hạ tầng | Nhóm node dùng chung cho nhiều Organization | Node được cấp riêng cho một Organization |
| Cách cấp tài nguyên | Hạn mức trên nhóm node dùng chung | Giữ chỗ CPU, bộ nhớ và số Pod trên node đã chọn |
| Vị trí workload | Trong nhóm node được phép | Đúng node đã chọn |
| Cơ chế Kubernetes | nodeSelector theo nhóm node | Node label và taint riêng, Pod mang toleration |
| Phù hợp với | Nhu cầu nhỏ và ngắn hạn | Cần tài nguyên ổn định và tách biệt hơn |

**Một Organization có thể dùng shared node, hoặc dùng dedicated node đã được cấp riêng cho mình.**

**Ghi chú thuyết trình**

Shared phù hợp với việc dùng chung dung lượng của nhóm node. Hạn mức Workspace giới hạn tài nguyên được yêu cầu, nhưng không đồng nghĩa với giữ sẵn lượng tài nguyên vật lý đó. Quản trị viên có thể cho phép tổng phân bổ cao hơn dung lượng khả dụng theo một tỉ lệ cấu hình. Khi nhiều môi trường cùng dùng hết hạn mức, một số Pod có thể phải chờ tài nguyên.

Dedicated cấp node cho một Organization và giữ chỗ một phần dung lượng trên node đó cho từng Workspace. Tổng phần giữ chỗ phải vừa với dung lượng còn lại sau khi trừ phần dành cho hệ thống. Một Workspace Dedicated chọn một node; các Workspace của cùng Organization có thể chia nhau dung lượng node này.

Ví dụ minh họa: node còn 8 CPU để cấp, hai Workspace giữ chỗ 4 CPU và 2 CPU thì còn 2 CPU. Chính sách Dedicated yêu cầu request bằng limit cho CPU và bộ nhớ. Đây là cơ chế hạch toán và bố trí workload; nó không cấp độc quyền từng lõi CPU vật lý và vẫn phụ thuộc vào tình trạng hoạt động của node.

**Định hướng hình ảnh:** Một bảng native năm hàng chiếm phần thân slide, cột Dedicated node tô nổi bật. Bỏ sơ đồ phân nhánh ở bản trước vì trùng nội dung với bảng; ý control plane dùng chung đưa xuống câu kết.

**Nguồn:** PRODUCT-PLAN §§5.3–5.4, 6; architecture/05-network-and-scheduling §§1–4; nhánh scaffold ADR-0017 và ADR-0104.

### Slide 20: Organization và phân bổ tài nguyên

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 10, mô hình và yêu cầu hệ thống

**Nội dung trên slide**

Organization là đơn vị quản lý thành viên, group và các Workspace của một cá nhân hoặc tổ chức.

Một sơ đồ gồm **Platform Admin** ở ngoài, mũi tên **ClusterGrant** đi vào khung **ORGANIZATION**.

Trong khung Organization:

- Ba ô số liệu của cả grant: Hạn mức 20 CPU · 40 GiB · Đã phân bổ 14 CPU · 28 GiB · Còn lại 6 CPU · 12 GiB
- Dòng giữa: Org Owner phân bổ hạn mức và gán group cho từng Workspace
- Hai vùng con theo chế độ compute:
  - **SHARED POOL** — trần mềm trên nhóm node dùng chung. Workspace A: 4 CPU · 8 GiB · Group A: Owner. Ghi chú: có thể overcommit, Pod chờ nếu pool hết chỗ.
  - **DEDICATED NODE · node-a1** — capacity 16 CPU · 32 GiB, còn lại 6 CPU · 12 GiB. Workspace B: 6 CPU · 12 GiB · Group B: Owner. Workspace C: 4 CPU · 8 GiB · Group C: Developer.

**Tổng phân bổ nằm trong hạn mức Organization. Riêng Dedicated node, tổng giữ chỗ không vượt capacity của node.**

**Ghi chú thuyết trình**

Organization là đơn vị dịch vụ dùng để quản lý thành viên, group và các Workspace của một cá nhân hoặc tổ chức. Tài khoản mới được tạo một Organization cá nhân; tài khoản đó cũng có thể được mời vào Organization khác.

**ClusterGrant:** Phần tài nguyên mà Platform Admin cấp cho một Organization trên một cluster gọi là ClusterGrant. Nó gồm hạn mức CPU, bộ nhớ, lưu trữ, số Workspace và các lựa chọn hạ tầng được phép dùng như nhóm node, vùng địa chỉ mạng, StorageClass và IngressClass. Org Owner chỉ phân bổ trong phần đã được cấp; muốn thêm thì Platform Admin phải cấp thêm.

**Ba ô số liệu:** Đây là số của cả ClusterGrant, không tách theo chế độ compute. Hạn mức là tổng được cấp, Đã phân bổ là tổng các Workspace đang giữ, Còn lại là phần có thể tạo Workspace mới. Trong ví dụ, ba Workspace giữ 4, 6 và 4 CPU nên tổng là 14 CPU và 28 GiB.

**Hai vùng con:** Cùng một hạn mức Organization, nhưng cách ràng buộc khác nhau theo chế độ compute. Shared pool là trần mềm: quota của Workspace là giới hạn trên, nhiều Organization dùng chung nhóm node và Platform Admin đặt hệ số overcommit; khi mọi Workspace cùng dùng hết quota, Pod có thể phải chờ. Dedicated node là giữ chỗ cứng: tổng reservation của các Workspace trên node không bao giờ vượt capacity thật của node đó, nên hệ số overcommit bằng 1. Đây là khác biệt bản chất giữa hai chế độ, đã được so sánh ở slide trước.

**Vai trò trên từng Workspace:** Mỗi Workspace nhận phần hạn mức riêng và các group được gán kèm vai trò. Owner quản lý namespace và tài nguyên trong Workspace, Developer triển khai workload, Viewer chỉ xem. Một group có thể được gán vào nhiều Workspace và một Workspace có thể có nhiều group với vai trò khác nhau; ba Workspace trên slide chỉ là ví dụ.

Organization quản lý việc sử dụng dịch vụ, còn Capsule Tenant quản lý các namespace của một Workspace bên trong Kubernetes. Vai trò và cơ chế phân quyền được trình bày kỹ ở slide 23.

**Định hướng hình ảnh:** Platform Admin đặt ngoài, bên trái, mũi tên thẳng có nhãn ClusterGrant đi vào khung Organization. Trong khung, hàng trên là ba ô số liệu bằng nhau của cả grant, giữa là một dòng mô tả hành động của Org Owner, dưới là hai vùng con bằng nhau cho Shared pool và Dedicated node, mỗi vùng có tiêu đề, một dòng ràng buộc và các dòng Workspace. Không vẽ Tenant hay namespace ở slide này.

**Nguồn:** PRODUCT-PLAN §§4–5, 8.3–10; architecture/02-data-model §§1–4.

### Slide 21: Cấp phát và thu hồi Workspace

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 12, sơ đồ xử lý trong hệ thống

**Nội dung trên slide**

**Tạo môi trường**

1. API kiểm tra quyền, hạn mức và tài nguyên khả dụng.
2. Ghi nhận phần tài nguyên được phân bổ cùng yêu cầu cấp phát.
3. Worker chuyển yêu cầu; Controller tạo Tenant và cấu hình mạng, quyền truy cập, hạn mức.
4. Cập nhật trạng thái; cung cấp thông tin truy cập khi Workspace sẵn sàng.

**Thu hồi môi trường**

Thu hồi quyền truy cập → Xóa namespace và tài nguyên liên quan → Hoàn trả phần tài nguyên được phân bổ.

**Tài nguyên chỉ được hoàn trả sau khi quá trình dọn dẹp tương ứng hoàn tất.**

**Ghi chú thuyết trình**

Việc ghi nhận tài nguyên và tạo tài nguyên Kubernetes diễn ra ở hai nơi. Để không nhận quá nhiều yêu cầu so với hạn mức, API kiểm tra và ghi nhận phần phân bổ trong cùng một giao dịch cơ sở dữ liệu. Trong chế độ Dedicated, bước này còn giữ chỗ dung lượng trên node đã chọn.

Giao dịch đồng thời ghi một công việc cần xử lý vào outbox, tức danh sách công việc bền vững trong PostgreSQL. Worker lấy công việc từ đó và chuyển cấu hình xuống cluster. Mỗi thao tác có bản ghi trạng thái riêng để theo dõi tiến trình và lỗi. Việc gửi lại yêu cầu hoặc thử lại được xử lý để không tạo trùng môi trường.

Nếu cấp phát chưa hoàn tất, phần tài nguyên đã ghi nhận vẫn được giữ cho yêu cầu đó tới khi xử lý tiếp hoặc dọn dẹp. Khi xóa Workspace, hệ thống xóa cả namespace, workload và dữ liệu lưu trữ gắn với môi trường theo chính sách xóa hiện tại. Portal yêu cầu xác nhận tên Workspace trước thao tác này.

Dọn dẹp phải đợi tài nguyên Kubernetes và mạng được giải phóng. Dải địa chỉ mạng còn qua một khoảng cách ly trước khi được cấp lại. Từ đó, hệ thống tránh cấp lại tài nguyên trong khi môi trường cũ vẫn đang sử dụng.

**Định hướng hình ảnh:** Hai luồng Tạo môi trường và Thu hồi môi trường. Ở luồng tạo, thể hiện điểm ghi nhận phân bổ trước khi tạo tài nguyên Kubernetes. Ở luồng thu hồi, đặt bước hoàn trả sau bước dọn dẹp. Trạng thái xử lý trả về Portal.

**Nguồn:** PRODUCT-PLAN §§5.3, 14, 17; architecture/03-flows §§5, 12–13; architecture/04-state-machines; nhánh scaffold ADR-0103 và ADR-0104.

### Slide 22: Mạng, lưu trữ và truy cập ứng dụng

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 14, các thành phần phục vụ môi trường

**Nội dung trên slide**

Mỗi Workspace có vùng mạng riêng, lưu trữ theo hạn mức và đường truy cập ứng dụng riêng.

- **Mạng:** Mỗi Workspace một IPPool riêng do Calico cấp. NetworkPolicy mặc định chặn lưu lượng giữa các Workspace.
- **Lưu trữ:** PVC dùng StorageClass được cấp, nằm trong hạn mức dung lượng của Workspace.
- **Truy cập ứng dụng:** Ingress dùng IngressClass được cấp. Hostname nằm trong vùng tên miền riêng của Workspace, chứng chỉ TLS do cert-manager cấp.

Sơ đồ bên phải: **User** → HTTPS → khung **WORKSPACE** gồm Ingress → Service → Pod, Pod → PVC. Nhãn khung ghi "IPPool riêng · NetworkPolicy mặc định".

Hostname minh họa: `web.acme-solar.apps.cluster-a.tenantry.example`

**Ghi chú thuyết trình**

Workload cần mạng để trao đổi dữ liệu, có thể cần lưu trữ bền vững và đường truy cập từ bên ngoài. Tenantry chuẩn bị các cấu hình hạ tầng này theo lựa chọn được cấp cho Workspace. Người dùng khai báo những tài nguyên ứng dụng cần sử dụng.

Mỗi Workspace được cấp dải địa chỉ Pod qua một Calico IPPool. Dải địa chỉ giúp tổ chức việc cấp IP; NetworkPolicy mới là cơ chế kiểm soát lưu lượng. Chính sách nền tảng chặn kết nối giữa Workspace và cho phép các luồng cần thiết như DNS hoặc lưu lượng từ ingress controller theo cấu hình. Các quy tắc và metadata dùng làm ranh giới phải được bảo vệ khỏi thay đổi làm mở rộng quyền truy cập.

PVC là yêu cầu lưu trữ của workload. Hệ thống giới hạn tổng dung lượng được yêu cầu theo loại lưu trữ được phép. Backend NFS subdir trong thiết kế không cưỡng chế số byte thực tế ghi xuống, nên hạn mức yêu cầu PVC và dung lượng thực tế sử dụng là hai đại lượng khác nhau.

Ingress ánh xạ hostname tới Service và workload. Tenantry cấp vùng hostname, phân phối cấu hình chứng chỉ và giới hạn tên miền được phép dùng. Hostname trên slide là ví dụ. Thiết lập chứng chỉ hiện dùng CA nội bộ, cần cấu hình tin cậy trên máy truy cập. Đây là đường vào ứng dụng; kubeconfig và Headlamp phục vụ thao tác quản lý Kubernetes.

**Định hướng hình ảnh:** Chia hai cột. Cột trái ba mục chữ, mỗi mục một tiêu đề đậm và hai đến ba dòng mô tả. Cột phải một đường đi duy nhất từ User xuống Ingress rồi Service, Pod và PVC, đặt trong khung Workspace; hostname minh họa nằm ngay dưới khung. Pod tô nền xanh đậm để thấy nơi workload chạy.

**Nguồn:** PRODUCT-PLAN §§7, 18; architecture/05-network-and-scheduling §§5–9; [Hạn chế của NFS provisioner](https://github.com/kubernetes-sigs/nfs-subdir-external-provisioner#nfs-provisioner-limitationspitfalls), [NetworkPolicy trong Kubernetes](https://kubernetes.io/docs/concepts/services-networking/network-policies/).

### Slide 23: Đăng nhập và phân quyền

**Phần:** Giải pháp đề xuất  
**Tham chiếu template:** Trang 14, giải thích cơ chế truy cập

**Nội dung trên slide**

Keycloak xác thực danh tính một lần. Quyền được cấp ở hai phạm vi tách biệt.

**Quyền trên Portal — vai trò nền tảng và vai trò trong Organization**

| Vai trò | Phạm vi thao tác |
| --- | --- |
| Platform Admin | Đăng ký cluster, tạo Organization, cấp ClusterGrant |
| OrgOwner | Quản lý thành viên, group, Workspace và hạn mức |
| OrgAdmin | Như OrgOwner, trừ chuyển quyền sở hữu và xóa Organization |
| OrgMember | Xem Organization và các Workspace được cấp quyền |

**Quyền trong Workspace — vai trò gán cho group theo từng Workspace**

| Vai trò | Phạm vi thao tác |
| --- | --- |
| TenantOwner | Tạo namespace, quản trị workload và RBAC trong Tenant |
| Developer | Quản lý workload trong namespace được phép |
| Viewer | Xem tài nguyên được cho phép, không đọc Secret |

**Quyền trên Portal và quyền thao tác Kubernetes được cấp riêng, không suy ra từ nhau.**

**Ghi chú thuyết trình**

Đăng nhập xác định người đang sử dụng dịch vụ; phân quyền xác định những việc họ được làm. Ví dụ, người quản lý Organization có thể phân bổ tài nguyên và cấu hình Workspace trên Portal. Để dùng kubectl trong Workspace đó, họ vẫn cần thuộc nhóm được gán quyền Kubernetes tương ứng.

Nhóm là tập hợp thành viên của Organization. Một nhóm có thể được cấp quyền trên các Workspace khác nhau. Dữ liệu nhóm nằm trong PostgreSQL và được đồng bộ sang Keycloak; Kubernetes sử dụng danh tính cùng thông tin nhóm để kiểm tra quyền.

TenantOwner được quản lý namespace trong Tenant, còn Developer triển khai workload trong namespace được cấp. Viewer chỉ đọc những tài nguyên cho phép; mẫu quyền Viewer loại trừ đọc Secret và thực thi lệnh trong container. Các vai trò đều chịu hạn mức và chính sách của môi trường.

kubeconfig chứa cấu hình kết nối và đăng nhập OIDC, một giao thức xác thực dựa trên danh tính. kubelogin hỗ trợ lấy token để kubectl kết nối. Headlamp dùng danh tính của người đăng nhập. Hai công cụ đi qua Capsule Proxy; token đăng nhập Portal có phạm vi sử dụng riêng. Khi thu hồi quyền, thời điểm có hiệu lực còn phụ thuộc đồng bộ nhóm và thời hạn token đã cấp.

**Định hướng hình ảnh:** Hai cột đối xứng, mỗi cột một nhãn phần, một dòng mô tả và một bảng native; bảng trái bốn vai trò, bảng phải ba vai trò, hai bảng kết thúc cùng một đường ngang. Không dùng sơ đồ phân nhánh từ Keycloak như bản trước; ý Keycloak xác thực một lần đưa lên câu dẫn đầu slide.

**Nguồn:** PRODUCT-PLAN §§8–12, 20; architecture/06-security-and-identity §§2–4, 6; nhánh scaffold `tenantry/web/src/features/workspaces/CreateWorkspacePage.tsx`.

### Slide 24: Thực nghiệm và Kết quả

**Phần:** Thực nghiệm và Kết quả  
**Tham chiếu template:** Trang 16–19

**Nội dung trên slide**

### Slide 25: Kết luận và hướng phát triển

**Phần:** Kết luận  
**Tham chiếu template:** Trang 20, kết luận bố cục ba cột

**Nội dung trên slide**

**Giải pháp đề xuất**

- Tenantry cung cấp Workspace trên cluster Kubernetes dùng chung.
- Capsule quản lý Tenant; Tenantry tổ chức việc cấp môi trường và quyền sử dụng thành dịch vụ.

**Phạm vi**

- Người dùng triển khai workload trong tài nguyên và quyền được cấp.
- Bên cung cấp vận hành cluster và các thành phần hạ tầng.

**Hướng phát triển**

- Mở rộng cấp môi trường trên nhiều cluster.
- Bổ sung thành phần cấp phát tại từng cluster để hỗ trợ triển khai phân tán.

**Ghi chú thuyết trình**

“Bài toán bắt đầu từ nhu cầu có một môi trường Kubernetes để sử dụng. Tenantry chọn cấp môi trường trên hạ tầng sẵn có, với Capsule làm nền tảng quản lý các namespace theo Tenant. Người dùng nhận Workspace, tài nguyên và quyền truy cập; bên cung cấp tổ chức việc cấp phát và vận hành hạ tầng.”

Thiết kế ban đầu tập trung vào một cluster. Hướng mở rộng là đưa thành phần cấp phát, gọi là Cluster Agent, tới từng cluster và để các thành phần này kết nối tới nền tảng trung tâm. Mô hình Organization, hạn mức và Workspace đã gắn với cluster cụ thể để hỗ trợ hướng phát triển đó.

**Định hướng hình ảnh:** Ba cột Giải pháp đề xuất, Phạm vi và Hướng phát triển theo bố cục kết luận của template. Dùng tông xanh nước biển/trắng đã thống nhất, chữ ngắn và khoảng trống rõ ràng.

**Nguồn:** PRODUCT-PLAN §§1–2, 13–14, 25.

### Slide 26: Cảm ơn

**Phần:** Hỏi đáp  
**Tham chiếu template:** Trang 21, bố cục kết thúc

**Nội dung trên slide**

**Cảm ơn**

Hỏi đáp và thảo luận

Tenantry — Dịch vụ cung cấp môi trường Kubernetes với Capsule

**Ghi chú thuyết trình**

Cảm ơn hội đồng và mời trao đổi về mô hình dịch vụ, cách quản lý tài nguyên và kiến trúc của Tenantry.

**Định hướng hình ảnh:** Bố cục kết thúc đơn giản theo template, dùng tông xanh nước biển/trắng cùng hình ảnh phù hợp với Kubernetes và Tenantry.

**Nguồn:** template.pdf tr.21.

## 4. Nguồn tham khảo và tài liệu cài đặt



### Sản phẩm và kiến trúc

- [PRODUCT-PLAN.md](PRODUCT-PLAN.md): phạm vi sản phẩm, các khái niệm dịch vụ, cơ chế grant, vòng đời Workspace, truy cập, tính toán, mạng, lưu trữ và định hướng đa cluster.
- [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md): cấu trúc triển khai và bối cảnh kỹ thuật hỗ trợ.
- [Mục lục kiến trúc](architecture/README.md): các góc nhìn về hệ thống, dữ liệu, luồng xử lý, trạng thái, mạng, định danh và UX, được dùng để đơn giản hóa các slide phần giải pháp.
- [template.pdf](template.pdf): chuẩn nền về hình ảnh và mạch trình bày, đặc biệt là trang 3 cho slide giới thiệu Kubernetes.



### Tham chiếu tới nhánh scaffold

Đọc trực tiếp từ nhánh `feat/tenantry-scaffold` tại commit `d7ab21cc470d84509e9e97e6ec564eb31de3cec5`, không chuyển nhánh và không thay đổi nhánh làm việc hiện tại:


| Đường dẫn trên nhánh                                           | Dùng cho                                                        |
| -------------------------------------------------------------- | --------------------------------------------------------------- |
| `tenantry/README.md`                                           | Cấu trúc thành phần và bối cảnh triển khai ở mức tổng quan      |
| `tenantry/api/go.mod`                                          | Các phụ thuộc của API Go, công cụ OpenAPI, pgx, goose           |
| `tenantry/controller/go.mod`                                   | Ngăn xếp controller-runtime của Kubernetes                      |
| `tenantry/web/package.json`                                    | React, TypeScript, Vite, Mantine và công cụ phía client         |
| `tenantry/docs/adr/0017-shared-soft-dedicated-hard.md`         | Mô hình cấp phát Shared và giữ chỗ Dedicated đã được chấp thuận |
| `tenantry/docs/adr/0102-codegen-and-persistence.md`            | Lựa chọn OpenAPI và SQL viết tay/pgx đã được chấp thuận         |
| `tenantry/docs/adr/0103-outbox-and-operation-tuning.md` | Cơ chế xử lý công việc và theo dõi trạng thái cấp phát |
| `tenantry/docs/adr/0104-platform-node-grant-crd.md`            | Cơ chế cấp phát PlatformNodeGrant đã được chấp thuận            |
| `tenantry/web/src/features/workspaces/CreateWorkspacePage.tsx` | Wizard tạo Workspace năm bước hiện có                           |


Các tham chiếu này hỗ trợ phần mô tả công nghệ và quy trình. Các slide không đánh đồng việc mã nguồn đã tồn tại với việc đã kiểm chứng xong khi chạy thực tế. Phần Thực nghiệm và Kết quả vẫn để trống theo yêu cầu.

### Nguồn tham khảo chính thức từ bên ngoài

Các nguồn bên ngoài hỗ trợ phần giới thiệu Kubernetes và phần so sánh các giải pháp hiện có. Đối chiếu trong các ngày 05–07/09/2026. Phần bàn về mức độ phù hợp và các đánh đổi là tổng hợp kiến trúc, không phải so sánh có đo đạc hay khảo sát thị trường đầy đủ.

- [Tổng quan Kubernetes](https://kubernetes.io/docs/concepts/overview/)
- [Các thành phần Kubernetes](https://kubernetes.io/docs/concepts/overview/components/)
- [Multi-tenancy trong Kubernetes](https://kubernetes.io/docs/concepts/security/multi-tenancy/)
- [Tổng quan Amazon EKS](https://docs.aws.amazon.com/eks/latest/userguide/what-is-eks.html)
- [Tổng quan Google Kubernetes Engine](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/kubernetes-engine-overview)
- [Tổng quan Azure Kubernetes Service](https://learn.microsoft.com/en-us/azure/aks/what-is-aks)
- [Tạo cluster với kubeadm](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/create-cluster-kubeadm/)
- [Tổng quan kind](https://kind.sigs.k8s.io/)
- [Bắt đầu với minikube](https://minikube.sigs.k8s.io/docs/start/)
- [Tổng quan Capsule](https://projectcapsule.dev/docs/overview/)
- [Hạn chế của NFS subdir provisioner](https://github.com/kubernetes-sigs/nfs-subdir-external-provisioner#nfs-provisioner-limitationspitfalls)
- [Ngữ nghĩa NetworkPolicy trong Kubernetes](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
