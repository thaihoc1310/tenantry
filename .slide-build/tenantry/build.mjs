import fs from 'node:fs/promises';
import path from 'node:path';
import {Presentation,PresentationFile} from '@oai/artifact-tool';
const ROOT='/home/thaihoc/Workspace/capsule';
const B=path.join(ROOT,'.slide-build/tenantry');
const P=Presentation.create({slideSize:{width:1920,height:1080}});
const C={navy:'#12325B',blue:'#2563CB',cyan:'#208AA5',ink:'#172B43',muted:'#576B82',light:'#F0F5FC',line:'#CAD7E7',white:'#FFFFFF',amber:'#956221'};
const F='Arimo',S=1.2;
const source=await fs.readFile(path.join(ROOT,'docs/SLIDE-CONTENT-DRAFT.md'),'utf8');
const sections=[...source.matchAll(/^### Slide (\d+): (.+)\n([\s\S]*?)(?=^### Slide |^## 4\.|$(?![\s\S]))/gm)];
const notes={};for(const m of sections){const body=m[3];notes[+m[1]]=(body.match(/\*\*Ghi chú thuyết trình\*\*\s*([\s\S]*?)(?=\*\*Định hướng hình ảnh|$)/)?.[1]??'').trim()+'\n\nNguồn: '+(body.match(/\*\*Nguồn:\*\* ([\s\S]*)/)?.[1]??'docs/SLIDE-CONTENT-DRAFT.md');}
const assets=JSON.parse(await fs.readFile(path.join(B,'assets/sources.json'),'utf8'));
const pos=(x,y,w,h)=>({left:x*S,top:y*S,width:w*S,height:h*S});
function text(s,t,x,y,w,h,size=28,color=C.ink,bold=false,align='left'){const sh=s.shapes.add({geometry:'textbox',name:t.slice(0,55),position:pos(x,y,w,h),fill:'none',line:{fill:'none',width:0}});sh.text=t;sh.text.style={typeface:F,fontSize:size*S,color,bold,alignment:align,verticalAlignment:'top',autoFit:'none',wrap:'square',insets:{left:0,right:0,top:0,bottom:0}};return sh;}
function rect(s,x,y,w,h,fill=C.light,stroke='none',dash=false){return s.shapes.add({geometry:'rect',position:pos(x,y,w,h),fill,line:{fill:stroke,width:stroke==='none'?0:1.6*S,style:dash?'dashed':'solid'}});}
function rule(s,x,y,w,color=C.line){return s.shapes.add({geometry:'line',position:pos(x,y,w,0),fill:'none',line:{fill:color,width:1.2*S}});}
function box(s,t,x,y,w,h,{fill=C.light,stroke=C.line,color=C.navy,size=26,bold=true,dash=false}={}){const r=rect(s,x,y,w,h,fill,stroke,dash);r.text=t;r.text.style={typeface:F,fontSize:size*S,color,bold,alignment:'center',verticalAlignment:'middle',wrap:'square',autoFit:'none',insets:{left:12*S,right:12*S,top:8*S,bottom:8*S}};return r;}
function link(s,a,b,from='right',to='left',color=C.blue,dashed=false,bi=false){const c=s.shapes.connect(a,b,{kind:'elbow',fromSide:from,toSide:to,line:{fill:color,width:2*S,style:dashed?'dashed':'solid'},tail:{type:'triangle',width:'med',length:'med'},...(bi?{head:{type:'triangle',width:'med',length:'med'}}:{})});c.bringToFront();return c;}
async function img(s,name,x,y,w,h){const p=path.join(B,'assets',name+'.png');const b=await fs.readFile(p);s.images.add({blob:b,contentType:'image/png',alt:name,fit:'contain',position:pos(x,y,w,h)});if(assets[name])s.speakerNotes.textFrame.setText((s.speakerNotes.textFrame.text??'')+'\nHình: '+assets[name]);}
function base(n,section,title,{size=48,blank=false}={}){const s=P.slides.add();s.background.fill=C.white;s.speakerNotes.textFrame.setText(notes[n]??'');if(!blank){text(s,section,64,31,1400,30,18,C.blue,true);text(s,title,64,84,1472,118,size,C.navy,true);rule(s,64,844,1472);text(s,'TENANTRY',64,861,600,22,14,C.muted,true);text(s,String(n).padStart(2,'0'),1450,854,85,34,22,C.blue,true,'right');}else{text(s,title,64,85,1472,135,54,C.navy,true);}return s;}
function takeaway(s,t,y=756,size=27){rule(s,64,y-18,1472,C.blue);text(s,t,64,y,1472,72,size,C.navy,true);}
function label(s,head,body,x,y,w,{size=28,bodySize=26,gap=48}={}){text(s,head,x,y,w,44,size,C.navy,true);text(s,body,x,y+gap,w,155,bodySize,C.ink);}
function nativeTable(s,vals,x,y,width,heights,cols,font=24,hl=-1){const height=heights.reduce((a,b)=>a+b,0);const t=s.tables.add({rows:vals.length,columns:vals[0].length,left:x*S,top:y*S,width:width*S,height:height*S,columnWidths:cols.map(v=>v*S),values:vals});t.borders.assign({fill:C.line,width:1,style:'solid'});for(let r=0;r<vals.length;r++){t.rows[r].height=heights[r]*S;for(let c=0;c<vals[r].length;c++){const cell=t.getCell(r,c);cell.fill=r===0?C.navy:r%2?C.light:C.white;if(r>0&&c===hl)cell.fill='#DCE9FA';cell.text.style={typeface:F,fontSize:font*S,color:r===0?C.white:(c===hl?C.navy:C.ink),bold:r===0||c===hl,alignment:'left',verticalAlignment:'middle',autoFit:'none',wrap:'square',insets:{left:15*S,right:15*S,top:10*S,bottom:10*S}};}}return t;}
// 01. Cover, following the template's left title and right illustration.
{
const s=P.slides.add();s.background.fill=C.white;s.speakerNotes.textFrame.setText(notes[1]+'\nHình minh họa hạ tầng: Imagegen.');
await img(s,'cover',840,126,720,720);
text(s,'ĐỒ ÁN TỐT NGHIỆP ĐẠI HỌC',64,68,1100,36,21,C.blue,true);
text(s,'Tenantry',64,215,780,125,104,C.navy,true);
text(s,'Kubernetes Tenant as a Service\nvới Capsule',69,354,805,123,42,C.navy,true);
text(s,'Truy cập tự phục vụ tới các\nmôi trường Kubernetes',69,497,725,100,30,C.muted);
rule(s,69,634,590,C.blue);
text(s,'[Tên tác giả]  ·  [Mã sinh viên / lớp]\nGiảng viên hướng dẫn: [Tên giảng viên]\n[Đơn vị đào tạo]\n[Ngày bảo vệ]',69,661,780,163,22,C.muted);
}
// 02. Five-part academic agenda, flat typography.
{
const s=base(2,'MỤC LỤC','Nội dung trình bày');
const titles=['Giới thiệu','Các giải pháp hiện có','Giải pháp đề xuất','Thực nghiệm và Kết quả','Kết luận và hướng phát triển'];
for(let i=0;i<5;i++){text(s,String(i+1).padStart(2,'0'),90,226+i*111,116,75,54,i===2?C.blue:C.muted,true);text(s,titles[i],248,240+i*111,1160,63,37,C.navy,i===2);rule(s,248,311+i*111,1190);}
}
// 03. Kubernetes introduction, equivalent to template page 3.
{
const s=base(3,'GIỚI THIỆU','Kubernetes');await img(s,'kubernetes',390,69,86,86);
label(s,'Nền tảng mã nguồn mở','Quản lý các workload container hóa.\nGoogle mã nguồn mở lần đầu năm 2014.',64,217,670,{bodySize:29});
label(s,'Tự động hóa vận hành','Triển khai, mở rộng quy mô và\nphục hồi sau sự cố.',64,381,665,{bodySize:29});
label(s,'Một API chung','Tài nguyên tính toán, mạng và lưu trữ.',64,546,665,{bodySize:29});
rect(s,812,220,724,473,C.light,C.line);text(s,'CLUSTER KUBERNETES',850,244,630,36,22,C.blue,true);
const cp=box(s,'Control plane\nQuản lý trạng thái cluster',958,309,431,104,{fill:C.navy,color:C.white,stroke:C.navy,size:27});
const w1=box(s,'Worker node\nChạy workload',846,518,303,113,{fill:C.white,size:26});const w2=box(s,'Worker node\nChạy workload',1199,518,303,113,{fill:C.white,size:26});link(s,cp,w1,'bottom','top');link(s,cp,w2,'bottom','top');
takeaway(s,'Một cluster bao gồm control plane cùng các worker node chạy workload.');
}
// 04. Three needs in identical boxes, centre-aligned so connectors run straight.
{
const s=base(4,'GIỚI THIỆU','Nhu cầu truy cập môi trường Kubernetes');
text(s,'Người dùng cần truy cập Kubernetes vì nhiều mục đích khác nhau:',64,199,1460,53,30,C.muted);
const central=box(s,'Môi trường\nKubernetes',651,382,315,150,{fill:C.navy,color:C.white,size:36});
const a=box(s,'Triển khai và vận hành\nsản phẩm hoặc dịch vụ',64,392,487,130,{fill:C.light,stroke:C.blue,size:30});await img(s,'rocket',275,312,64,64);
const b=box(s,'Thực hiện các tác vụ ngắn hạn:\nxử lý dữ liệu, kiểm thử, chạy thử',1010,392,526,130,{fill:C.light,stroke:C.blue,size:30});await img(s,'timer',1241,312,64,64);
const c=box(s,'Học Kubernetes và thử nghiệm với các API',463,612,690,86,{fill:C.light,stroke:C.blue,size:30});await img(s,'graduation-cap',368,619,72,72);
link(s,a,central);link(s,b,central,'left','right');link(s,c,central,'top','bottom');
takeaway(s,'Điểm chung: một môi trường dùng được, có tài nguyên và quyền truy cập.');
}
// 05. The focus need in detail: four examples on the left, three shared traits on the right.
{
const s=base(5,'GIỚI THIỆU','Nhu cầu sử dụng Kubernetes ngắn hạn');
text(s,'Nhiều công việc chỉ cần môi trường Kubernetes trong thời gian ngắn.',64,196,1472,50,29,C.muted);
const ex=[['flask-conical','Kiểm thử ứng dụng','Môi trường test, staging tạm\nđể chạy thử phiên bản mới.'],['presentation','Demo và PoC','Dựng bản thử nghiệm để trình bày\nvà đánh giá trước khi áp dụng.'],['users','Thực hành và workshop','Môi trường cho nhiều người\ntrong một buổi hoặc vài ngày.'],['database','Xử lý dữ liệu, tính toán','Chạy xử lý dữ liệu hoặc\ntính toán ngắn hạn.']];
for(let i=0;i<4;i++){const x=64+(i%2)*470,y=300+Math.floor(i/2)*200;await img(s,ex[i][0],x,y+2,52,52);text(s,ex[i][1],x+72,y,390,40,26,C.navy,true);text(s,ex[i][2],x+72,y+42,390,76,22,C.ink);}
rect(s,1060,262,476,452,C.light,C.line);
text(s,'ĐẶC ĐIỂM CHUNG',1092,284,420,38,23,C.blue,true);
const tr=[['Thời gian ngắn','Dùng từ vài giờ đến vài tuần.'],['Lặp lại nhiều lần','Mỗi lần cần một môi trường mới.'],['Chỉ cần chạy workload','Không cần quản trị cluster.']];
for(let i=0;i<3;i++){label(s,tr[i][0],tr[i][1],1092,350+i*118,420,{size:25,bodySize:22,gap:36});}
takeaway(s,'Thời gian sử dụng ngắn, nhưng công sức chuẩn bị môi trường không giảm theo và lặp lại mỗi lần.',766,26);
}
// 06. Preparation barrier is visible in a single process.
{
const s=base(6,'GIỚI THIỆU','Rào cản khi tiếp cận môi trường Kubernetes',{size:46});
text(s,'Trước khi bắt đầu, môi trường cần được chuẩn bị đầy đủ.',64,200,1430,58,31,C.muted);
const a=box(s,'Nhu cầu\nsử dụng',64,390,268,115,{fill:C.white,size:31});
const prep=rect(s,429,299,734,367,C.light,C.line);
text(s,'CHUẨN BỊ MÔI TRƯỜNG',467,320,655,45,26,C.blue,true);
label(s,'Tài nguyên','CPU, bộ nhớ và lưu trữ phù hợp',488,386,611,{size:27,bodySize:25,gap:38});
label(s,'Quyền truy cập','Thông tin đăng nhập và quyền quản lý workload',488,475,632,{size:27,bodySize:25,gap:38});
label(s,'Cấu hình môi trường','Kết nối mạng và các thành phần hỗ trợ',488,564,632,{size:27,bodySize:25,gap:38});
const b=box(s,'Bắt đầu sử dụng\nKubernetes',1255,390,281,115,{fill:C.navy,color:C.white,size:29});link(s,a,prep);link(s,prep,b);
text(s,'Thiết lập và duy trì đòi hỏi thời gian, kiến thức và công sức quản trị.',430,686,1105,48,26,C.muted);
takeaway(s,'Bài toán: cung cấp môi trường sẵn sàng sử dụng, giảm công sức chuẩn bị hạ tầng.',766,27);
}
// 07. Four goals, with one coherent environment concept.
{
const s=base(7,'GIỚI THIỆU','Mục tiêu đồ án');
const goals=[['Tự phục vụ','Yêu cầu quyền truy cập Kubernetes trên hạ tầng sẵn có.','key-round'],['Cấu hình theo nhu cầu','Cấp môi trường với tài nguyên và quyền truy cập có thể cấu hình.','sliders-horizontal'],['Công cụ tiêu chuẩn','Hỗ trợ công cụ Kubernetes và workload do người dùng tự chọn.','terminal'],['Quản lý tập trung','Cấp phát, cập nhật và dọn dẹp môi trường qua một nền tảng.','refresh-cw']];
for(let i=0;i<4;i++){await img(s,goals[i][2],65,211+i*123,65,65);text(s,goals[i][0],167,211+i*123,520,47,31,C.navy,true);text(s,goals[i][1],167,258+i*123,1333,54,29,C.ink);}
takeaway(s,'Người dùng yêu cầu môi trường và nhận quyền truy cập, nền tảng vận hành cluster.',764,28);
}
// 08. What the services do, effective for, limitations tied to this problem.
{
const s=base(8,'CÁC GIẢI PHÁP HIỆN CÓ','Dịch vụ Managed Kubernetes');
for(const [name,label,x]of [['amazon','Amazon EKS',64],['google','Google GKE',525],['azure','Microsoft AKS',1005]]){await img(s,name,x,194,73,70);text(s,label,x+94,211,370,47,31,C.navy,true);}text(s,'…',1465,211,65,48,34,C.muted);
text(s,'Cung cấp cluster để chạy workload. Nhà cung cấp cài đặt và vận hành control plane.',64,294,1472,88,30,C.ink);
rule(s,64,387,1472);
text(s,'Phù hợp với',64,419,567,49,32,C.blue,true);text(s,'Hạn chế',738,419,796,49,32,C.blue,true);
label(s,'Production trên cloud','Cần mở rộng và tích hợp dịch vụ cloud.',64,492,570,{size:29,bodySize:28,gap:43});
label(s,'Cluster độc lập','Cần cấu hình và vòng đời riêng.',64,627,570,{size:29,bodySize:28,gap:43});
const list=[['Tài nguyên và chi phí','Cluster riêng có thể dư tài nguyên, tăng chi phí cho nhu cầu nhỏ.'],['Thời gian chuẩn bị','Cluster mới cần thời gian và công sức khởi tạo, cấu hình.'],['Môi trường dùng chung','Cần bổ sung cấp môi trường, hạn mức và quyền truy cập cho từng người dùng.']];
for(let i=0;i<3;i++){text(s,list[i][0],738,491+i*104,790,38,27,C.navy,true);text(s,list[i][1],738,530+i*104,790,72,26,C.ink);}
}
// 09. Two groups retained within one slide.
{
const s=base(9,'CÁC GIẢI PHÁP HIỆN CÓ','Tự triển khai Kubernetes');
await img(s,'server',68,191,76,76);await img(s,'laptop',865,191,76,76);
text(s,'Cluster tự quản lý',167,202,553,50,34,C.navy,true);text(s,'Kubernetes local',965,202,571,50,34,C.navy,true);
text(s,'Ví dụ: kubeadm',167,251,553,38,25,C.blue);text(s,'Ví dụ: kind, minikube',965,251,571,38,25,C.blue);
text(s,'Tự xây dựng cluster trên\nhạ tầng do mình quản lý.',64,316,677,97,30);text(s,'Tạo cluster trên máy cá nhân\nđể học tập và thử nghiệm.',862,316,674,97,30);
rule(s,64,433,677);rule(s,862,433,674);
label(s,'Phù hợp với','Chủ động cấu hình hạ tầng và\ncác thành phần Kubernetes.',64,460,677,{bodySize:28});
label(s,'Phù hợp với','Học Kubernetes và thử nghiệm\nvới các API.',862,460,674,{bodySize:28});
label(s,'Hạn chế','Tự chuẩn bị tài nguyên, cài đặt,\nnâng cấp và xử lý sự cố cluster.\n\nNhiều người dùng cần thêm cơ chế\ncấp môi trường, tài nguyên và quyền.',64,622,677,{bodySize:26,gap:43});
label(s,'Hạn chế','Tài nguyên và thời gian hoạt động\nphụ thuộc vào máy cá nhân.\n\nChia sẻ cần thêm truy cập từ xa\nvà quyền sử dụng.',862,622,674,{bodySize:26,gap:43});
}
const CMP=[['Nhu cầu sử dụng','Chủ yếu dài hạn','Chủ yếu dài hạn'],['Vận hành cluster','Nhà cung cấp vận hành control plane','Người dùng tự cài đặt, nâng cấp, xử lý sự cố'],['Yêu cầu môi trường mới','Khởi tạo cluster mới','Chuẩn bị máy và cài đặt cluster'],['Tài nguyên cấp phát','Cả cluster, tính theo node','Cả cluster, trên máy tự chuẩn bị'],['Cấp cluster cho nhiều người dùng','Tự thiết lập thêm cơ chế cấp môi trường và quyền','Tự thiết lập thêm cơ chế cấp môi trường và quyền'],['Vòng đời môi trường','Người dùng tự quản lý theo từng cluster','Người dùng tự quản lý']];
const CMPH=['Tiêu chí','Managed Kubernetes','Cluster tự quản lý'];
const CMPW=[272,400,400,400],CMPR=[54,82,82,82,82,82,82];
// 10. Summary table of existing approaches; last column is the expectation.
{
const s=base(10,'CÁC GIẢI PHÁP HIỆN CÓ','So sánh các cách có môi trường Kubernetes',{size:44});
const exp=['Ngắn hạn và dài hạn','Bên cung cấp dịch vụ đảm nhiệm','Cấp trên cluster sẵn có','Hạn mức tài nguyên theo yêu cầu, trên hạ tầng dùng chung','Tích hợp sẵn cơ chế cấp môi trường và quyền','Cấp phát, cập nhật, dọn dẹp qua một nền tảng'];
nativeTable(s,[[...CMPH,'Kỳ vọng'],...CMP.map((r,i)=>[...r,exp[i]])],64,190,1472,CMPR,CMPW,22,3);
takeaway(s,'Thay vì cấp cả cluster, kỳ vọng là cấp môi trường theo hạn mức trên cluster sẵn có.',772,26);
}
// 11. Smaller scope: division inside an already shared cluster.
{
const s=base(11,'CÁC GIẢI PHÁP HIỆN CÓ','Chia sẻ cluster bằng namespace');
text(s,'Cách tiếp cận',64,203,700,48,32,C.blue,true);
text(s,'Cấp một hoặc nhiều namespace cho từng người hoặc nhóm. Kết hợp quyền truy cập, quota và network policy.',64,259,710,156,30);
rect(s,882,204,654,428,C.light,C.line);text(s,'CLUSTER DÙNG CHUNG',918,223,570,37,23,C.blue,true);
rect(s,917,287,272,292,C.white,C.line,true);rect(s,1223,287,274,292,C.white,C.line,true);
text(s,'Nhóm A',936,305,235,36,27,C.navy,true,'center');text(s,'Nhóm B',1244,305,233,36,27,C.navy,true,'center');
box(s,'namespace a-dev',938,367,230,60,{size:22});box(s,'namespace a-test',938,476,230,60,{size:22});box(s,'namespace b-dev',1244,367,230,60,{size:22});box(s,'namespace b-test',1244,476,230,60,{size:22});
text(s,'Quyền, quota và chính sách cho từng namespace',901,648,620,79,23,C.muted,false,'center');
text(s,'Hạn chế',64,446,700,48,32,C.blue,true);
label(s,'Thiếu quản lý namespace theo nhóm','Không có sẵn đơn vị gom namespace theo nhóm\nvà áp dụng chính sách nhất quán cho cả nhóm.',64,504,770,{size:28,bodySize:26,gap:47});
label(s,'Bùng nổ namespace','Quản trị viên trở thành điểm nghẽn khi phải\ntạo và cấu hình thủ công từng namespace.',64,666,770,{size:28,bodySize:26,gap:47});
}
// 12. Capsule grouping and tenant-owner namespace self-service.
{
const s=base(12,'CÁC GIẢI PHÁP HIỆN CÓ','Capsule: Quản lý namespace theo Tenant',{size:46});
text(s,'Capsule là Kubernetes Operator hỗ trợ quản lý nhiều người dùng hoặc nhóm trên cluster dùng chung.',64,195,1472,89,29);
text(s,'Tenant gom một hoặc nhiều namespace, cùng quyền hạn, hạn mức tài nguyên và chính sách.',64,289,710,126,30,C.navy,true);
const steps=[['01','Cluster Admin','Tạo Tenant, chỉ định người quản lý,\nhạn mức và chính sách.'],['02','Tenant Owner','Tự tạo và quản lý namespace\ntrong phạm vi được cấp.'],['03','Capsule','Tự động áp dụng cấu hình khai báo,\nkể cả cho namespace mới.']];
for(let i=0;i<3;i++){text(s,steps[i][0],64,425+i*110,80,52,32,C.blue,true);text(s,steps[i][1],165,420+i*110,570,40,28,C.navy,true);text(s,steps[i][2],165,462+i*110,620,65,25);}
rect(s,879,307,657,373,C.light,C.blue);text(s,'CAPSULE TENANT',915,330,577,41,27,C.blue,true);
text(s,'Quyền hạn, hạn mức, chính sách chung',915,389,580,66,25,C.navy);
box(s,'namespace dev',914,474,271,87,{fill:C.white,size:25});box(s,'namespace test',1216,474,281,87,{fill:C.white,size:25});
text(s,'Namespace mới nhận cấu hình đã khai báo',915,599,577,52,24,C.blue,true,'center');
takeaway(s,'Capsule là thành phần nền tảng quan trọng của dịch vụ Tenantry.',772,28);
}
// 13. What "one environment" is when a cluster is shared: namespace, Capsule Tenant, expectation.
{
const s=base(13,'CÁC GIẢI PHÁP HIỆN CÓ','So sánh cơ chế shared cluster');
nativeTable(s,[['Tiêu chí','Namespace','Capsule Tenant','Kỳ vọng'],['Đơn vị môi trường','Một hoặc nhiều namespace là một môi trường','Một Tenant gom và quản lý các namespace là một môi trường','Một đơn vị gom và quản lý các namespace, như Capsule Tenant'],['Đơn vị quản lý môi trường','Không có đơn vị quản lý các môi trường','Không có đơn vị quản lý các môi trường','Đơn vị Tổ chức quản lý các môi trường'],['Cấp phát tài nguyên','Quota cho từng namespace','Quota cho từng Tenant','Cấp cho tổ chức, chia cho các môi trường'],['Phân quyền','Cho từng namespace','Cho Tenant qua các group','Cho môi trường, theo vai trò, qua các group'],['Vòng đời môi trường','Quản trị viên tạo, sửa, xóa từng namespace','Quản trị viên tạo, sửa, xóa Tenant','Người dùng tự tạo, sửa, xóa môi trường dựa trên giới hạn trong tổ chức'],['IPPool riêng','Không hỗ trợ sẵn','Không hỗ trợ sẵn','Người dùng tự chọn CIDR và IPPool cho môi trường, có cơ chế chống trùng'],['Node cho môi trường','Quản trị viên đặt nodeSelector cho từng namespace','Đặt nodeSelector cho cả Tenant','Chọn shared node hoặc dedicated node của tổ chức, tự tính remaining resource, auto label các node'],['Truy cập','Kubeconfig do quản trị viên chuẩn bị','Kubeconfig do quản trị viên chuẩn bị, qua Capsule Proxy','Kubeconfig và giao diện web cấp sẵn']],64,180,1472,[44,64,64,64,64,64,64,74,64],[222,370,370,510],19,3);
takeaway(s,'Capsule Tenant quản lý các namespace và gom nhóm thành một môi trường khá tốt, nhưng việc quản lý các môi trường, cấp tài nguyên, mạng, node và kết nối cho từng môi trường vẫn cần một lớp dịch vụ.',780,23);
}
// 14. Definition plus the six capabilities that answer slide 13's expectation column.
{
const s=base(14,'GIẢI PHÁP ĐỀ XUẤT','Tenantry: Dịch vụ cung cấp môi trường Kubernetes',{size:42});
text(s,'Tenantry là lớp dịch vụ cấp và quản lý môi trường Kubernetes trên cluster dùng chung.',64,196,1472,50,30,C.navy,true);
text(s,'Mỗi môi trường là một Workspace, quản lý nhiều namespace.\nWorkspace được hiện thực bằng một Capsule Tenant, kèm các cơ chế cấp hạn mức tài nguyên, mạng, node và quyền truy cập riêng.',64,256,1472,112,27,C.ink);
rule(s,64,376,1472);
const caps=[['Organization & quota','Tài nguyên cấp cho Organization, chia cho các Workspace.'],['Self-service lifecycle','Người dùng tự tạo, sửa, xóa Workspace trong giới hạn được cấp.'],['Network & IPPool','Mỗi Workspace một CIDR và IPPool riêng, có cơ chế chống trùng.'],['Shared & dedicated node','Chọn node của Organization, tự tính remaining resource và auto label.'],['Role-based access','Gán quyền cho Workspace theo vai trò, qua các group.'],['kubeconfig & Web UI','kubeconfig và giao diện web cấp sẵn theo Workspace.']];
for(let i=0;i<6;i++){const x=64+(i%3)*500,y=404+Math.floor(i/3)*160;rect(s,x,y,472,144,C.light,C.line);text(s,caps[i][0],x+22,y+22,428,38,25,C.navy,true);text(s,caps[i][1],x+22,y+64,428,64,22,C.ink);}
takeaway(s,'Người dùng chọn công việc cần chạy, bên cung cấp vận hành cluster bên dưới.',772,27);
}
// 15. Key requirements the system must satisfy.
{
const s=base(15,'GIẢI PHÁP ĐỀ XUẤT','Yêu cầu hệ thống');
text(s,'Hệ thống cần đáp ứng các yêu cầu chính sau:',64,194,1472,46,29,C.muted);
text(s,'YÊU CẦU CHỨC NĂNG',64,258,690,36,23,C.blue,true);text(s,'YÊU CẦU PHI CHỨC NĂNG',796,258,740,36,23,C.blue,true);
rule(s,64,298,690);rule(s,796,298,740);
const fn=[['Tự phục vụ','Tạo, sửa, xóa Workspace qua Portal trong hạn mức được cấp.'],['Quản lý tổ chức','Mời thành viên, tạo nhóm và gán vai trò trong Organization.'],['Cấp phát tài nguyên','Chia hạn mức tổ chức cho từng Workspace, không vượt tổng.'],['Mạng và node','Cấp CIDR, IPPool riêng và chọn shared hoặc dedicated node.'],['Truy cập','Cấp kubeconfig và giao diện web theo danh tính người dùng.']];
const nf=[['Cách ly','Workspace tách biệt về tài nguyên, quyền và mạng.'],['Nhất quán','Trạng thái trên Portal khớp trạng thái thật trong cluster.'],['Tương thích','Dùng công cụ Kubernetes tiêu chuẩn cho workload của người dùng.'],['Mở rộng','Mô hình dữ liệu sẵn sàng cho nhiều cluster.'],['Theo dõi','Ghi nhận trạng thái cấp phát và nhật ký thao tác.']];
for(let i=0;i<5;i++){const y=316+i*86;label(s,fn[i][0],fn[i][1],64,y,690,{size:24,bodySize:21,gap:34});label(s,nf[i][0],nf[i][1],796,y,740,{size:24,bodySize:21,gap:34});}
takeaway(s,'Các yêu cầu này định hình kiến trúc và các cơ chế được trình bày ở phần sau.',772,27);
}
// 16. Same table as slide 10, Tenantry replaces the expectation column.
{
const s=base(16,'GIẢI PHÁP ĐỀ XUẤT','Tenantry so với các cách hiện có');
const ten=['Ngắn hạn và dài hạn','Bên cung cấp dịch vụ vận hành cluster dùng chung','Cấp Workspace trên cluster sẵn có','Hạn mức tài nguyên theo yêu cầu cho từng Workspace, trên shared node hoặc dedicated node','Hệ thống cấp Workspace, quyền truy cập và phương thức truy cập qua kubeconfig hoặc giao diện web','Hệ thống cấp phát, theo dõi trạng thái và thu hồi Workspace'];
nativeTable(s,[[...CMPH,'Tenantry'],...CMP.map((r,i)=>[...r,ten[i]])],64,186,1472,[54,78,78,78,78,78,78],[248,372,372,480],22,3);
takeaway(s,'Tenantry cấp Workspace theo hạn mức trên cluster sẵn có, đúng với cột Kỳ vọng ở phần trước.',772,26);
}
// 17. Component architecture: cluster frame only, colour-coded links, one legend row.
{
const s=base(17,'GIẢI PHÁP ĐỀ XUẤT','Kiến trúc tổng thể Tenantry');
const dash=(x1,y1,x2,y2)=>s.shapes.add({geometry:'line',position:pos(Math.min(x1,x2),Math.min(y1,y2),Math.abs(x2-x1),Math.abs(y2-y1)),fill:'none',line:{fill:C.cyan,width:1.8*S,style:'dashed'}});
const anchor=(x,y)=>rect(s,x,y,0.1,0.1,'none','none');
rect(s,300,456,1236,330,C.white,C.line);
text(s,'CLUSTER KUBERNETES',1000,468,520,28,18,C.blue,true,'right');
const user=box(s,'User',64,212,180,104,{fill:C.white,size:26});
const kc=box(s,'Keycloak',64,344,180,80,{fill:C.white,size:27});
const cli=box(s,'kubectl,\nHeadlamp',64,500,180,104,{fill:C.white,size:25});
const portal=box(s,'Portal UI',316,212,230,104,{fill:C.light,size:27});
const api=box(s,'Platform API',630,212,250,104,{fill:C.light,size:27});
const db=box(s,'Database',960,212,230,104,{fill:C.light,size:27});
const worker=box(s,'Provisioning\nWorker',1270,212,250,104,{fill:C.light,size:26});
const proxy=box(s,'Capsule Proxy',316,500,230,104,{fill:C.white,size:26});
const kapi=box(s,'Kubernetes API',793,500,250,104,{fill:C.navy,stroke:C.navy,color:C.white,size:27});
const wc=box(s,'Workspace\nController',1270,500,250,104,{fill:C.light,size:26});
const other=rect(s,316,672,1204,86,C.white,C.line);
text(s,'Other Components',316,684,1204,34,25,C.navy,true,'center');
const oit=text(s,'(etcd, kube-scheduler, kubelet, Capsule Operator, Calico hoặc Cilium, …)',316,720,1204,28,19,C.muted,false,'center');
oit.text.style={typeface:F,fontSize:19*S,color:C.muted,italic:true,alignment:'center',verticalAlignment:'top',autoFit:'none',wrap:'square',insets:{left:0,right:0,top:0,bottom:0}};
link(s,user,portal);
link(s,portal,api);
link(s,api,db,'right','left',C.amber,true,true);
link(s,db,worker,'right','left',C.amber,true,true);
link(s,worker,kapi,'bottom','top',C.blue,false,true);
link(s,wc,kapi,'left','right',C.blue,false,true);
link(s,cli,proxy);
link(s,proxy,kapi);
link(s,kapi,other,'bottom','top',C.blue,false,true);
link(s,kc,cli,'bottom','top',C.cyan,true,true);
dash(244,384,755,384);dash(755,360,755,384);dash(431,384,431,464);
link(s,anchor(755,360),api,'top','bottom',C.cyan,true);
link(s,anchor(431,464),proxy,'bottom','top',C.cyan,true);
const leg=(x,color,dashed,t,tw)=>{link(s,anchor(x,810),anchor(x+70,810),'right','left',color,dashed);text(s,t,x+84,797,tw,28,18,color,true);};
leg(238,C.blue,false,'HTTP: gọi API và thao tác Kubernetes',366);
leg(728,C.amber,true,'TCP: đọc ghi cơ sở dữ liệu',264);
leg(1116,C.cyan,true,'OIDC: đăng nhập và xác thực token',336);
}
// 18. The PlatformWorkspace CR: the single contract between control plane and cluster.
{
const s=base(18,'GIẢI PHÁP ĐỀ XUẤT','Custom Resource PlatformWorkspace');
text(s,'Mọi thay đổi Tenantry ghi vào cluster đi qua một Custom Resource mô tả trạng thái mong muốn của Workspace.',64,194,1472,46,28,C.muted);
text(s,'apiVersion: tenantry.io/v1alpha1 · kind: PlatformWorkspace',64,256,860,30,20,C.blue,true);
rect(s,64,296,760,436,C.light,C.line);
const spec=[['spec.organizationId','workspaceId, clusterId — định danh môi trường'],['spec.resources','Hạn mức CPU, bộ nhớ, Pod và dung lượng'],['spec.namespaces','Danh sách namespace mong muốn'],['spec.compute','Shared hoặc Dedicated, kèm node được chọn'],['spec.network','CIDR và IPPool của Workspace'],['spec.ingress','Vùng hostname và IngressClass được phép'],['spec.accessBindings','Group kèm vai trò TenantOwner, Developer, Viewer'],['status','phase, observedGeneration và tài nguyên đã tạo']];
for(let i=0;i<8;i++){const y=314+i*53;text(s,spec[i][0],88,y,246,30,21,C.navy,true);text(s,spec[i][1],342,y+2,466,30,19,C.ink);}
text(s,'RECONCILE LOOP',880,256,656,30,20,C.blue,true);
const steps=[['01','Provisioning Worker apply CR','Trạng thái mong muốn từ cơ sở dữ liệu'],['02','Workspace Controller watch','Nhận thay đổi qua Kubernetes API'],['03','Tạo và cập nhật tài nguyên','Capsule Tenant, RBAC, NetworkPolicy, IPPool, Certificate'],['04','Ghi status','Worker đọc lại, Portal hiển thị trạng thái']];
for(let i=0;i<4;i++){const y=296+i*106;rect(s,880,y,656,86,C.white,C.line);text(s,steps[i][0],902,y+26,44,34,24,C.blue,true);text(s,steps[i][1],956,y+14,560,30,23,C.navy,true);text(s,steps[i][2],956,y+46,560,30,19,C.ink);}
takeaway(s,'Portal ghi trạng thái mong muốn, controller đưa cluster về đúng trạng thái đó và báo lại qua status.',772,26);
}
// 19. Compute modes as one native table, shared control plane stated in the takeaway.
{
const s=base(19,'GIẢI PHÁP ĐỀ XUẤT','Shared node và Dedicated node');
text(s,'Khi tạo Workspace, người dùng chọn chế độ compute trong phạm vi Organization được cấp.',64,194,1472,46,29,C.muted);
nativeTable(s,[['Tiêu chí','Shared node','Dedicated node'],['Hạ tầng','Nhóm node dùng chung cho nhiều Organization','Node được cấp riêng cho một Organization'],['Cách cấp tài nguyên','Hạn mức trên nhóm node dùng chung','Giữ chỗ CPU, bộ nhớ và số Pod trên node đã chọn'],['Vị trí workload','Trong nhóm node được phép','Đúng node đã chọn'],['Cơ chế Kubernetes','nodeSelector theo nhóm node','Node label và taint riêng, Pod mang toleration'],['Phù hợp với','Nhu cầu nhỏ và ngắn hạn','Cần tài nguyên ổn định và tách biệt hơn']],64,268,1472,[60,82,82,82,82,82],[300,586,586],24,2);
takeaway(s,'Một Organization có thể dùng shared node, hoặc dùng dedicated node đã được cấp riêng cho mình.',772,26);
}
// 20. Organization: one grant quota on top, shared pool and dedicated node below.
{
const s=base(20,'GIẢI PHÁP ĐỀ XUẤT','Organization và phân bổ tài nguyên');
text(s,'Organization là đơn vị quản lý thành viên, group và các Workspace của một cá nhân hoặc tổ chức.',64,192,1472,46,28,C.muted);
const admin=box(s,'Platform Admin',64,330,240,92,{fill:C.white,size:25});
rect(s,420,248,1116,500,C.white,C.line);
text(s,'ORGANIZATION',452,262,500,30,20,C.blue,true);
const stat=[['Hạn mức','20 CPU · 40 GiB'],['Đã phân bổ','14 CPU · 28 GiB'],['Còn lại','6 CPU · 12 GiB']];
for(let i=0;i<3;i++){const x=452+i*358;rect(s,x,302,334,62,C.light,C.line);text(s,stat[i][0],x,310,334,24,18,C.muted,false,'center');text(s,stat[i][1],x,330,334,30,23,C.navy,true,'center');}
text(s,'Org Owner phân bổ hạn mức và gán group cho từng Workspace',452,380,1052,30,22,C.blue,true,'center');
const zone=(x,w,head,note,rows)=>{rect(s,x,422,w,302,C.white,C.line);text(s,head,x+20,440,w-40,28,20,C.blue,true);text(s,note,x+20,472,w-40,28,19,C.muted);
 rows.forEach((r,i)=>{const y=516+i*96;rect(s,x+20,y,w-40,84,C.light,C.line);text(s,r[0],x+38,y+26,180,30,22,C.navy,true);text(s,r[1],x+228,y+18,w-268,54,19,C.ink);});};
zone(452,512,'SHARED POOL','Trần mềm trên nhóm node dùng chung',[['Workspace A','4 CPU · 8 GiB\nGroup A: Owner']]);
zone(992,512,'DEDICATED NODE','Capacity 16 CPU · 32 GiB · Còn lại 6 CPU · 12 GiB',[['Workspace B','6 CPU · 12 GiB\nGroup B: Owner'],['Workspace C','4 CPU · 8 GiB\nGroup C: Developer']]);
link(s,admin,rect(s,416,376,0.1,0.1,'none','none'),'right','left');text(s,'ClusterGrant',306,338,112,26,17,C.muted,false,'center');
takeaway(s,'Tổng phân bổ nằm trong hạn mức Organization. Riêng Dedicated node, tổng giữ chỗ không vượt capacity của node.',772,25);
}
// 21. Ordered create/delete lifecycle.
{
const s=base(21,'GIẢI PHÁP ĐỀ XUẤT','Cấp phát và thu hồi Workspace');
text(s,'TẠO MÔI TRƯỜNG',64,213,1472,44,26,C.blue,true);
const create=[['01  Kiểm tra','Quyền, hạn mức và\ntài nguyên khả dụng'],['02  Ghi nhận','Phân bổ tài nguyên\nvà yêu cầu cấp phát'],['03  Cấp phát','Worker gửi yêu cầu.\nController tạo Tenant\nvà cấu hình môi trường.'],['04  Sẵn sàng','Cập nhật trạng thái.\nCấp thông tin truy cập\nkhi môi trường sẵn sàng.']];
let prev;for(let i=0;i<4;i++){const x=64+i*384;const sh=box(s,create[i][0],x,300,334,83,{fill:i===3?C.navy:C.light,color:i===3?C.white:C.navy,size:28});if(prev)link(s,prev,sh);prev=sh;text(s,create[i][1],x,412,334,118,27);}
text(s,'THU HỒI MÔI TRƯỜNG',64,564,1472,44,26,C.blue,true);
const a=box(s,'Thu hồi quyền truy cập',64,639,409,89,{fill:C.white,size:27});const b=box(s,'Xóa namespace và\ntài nguyên liên quan',599,639,426,89,{fill:C.white,size:26});const c=box(s,'Hoàn trả tài nguyên\nđã phân bổ',1152,639,384,89,{fill:C.light,size:26});link(s,a,b);link(s,b,c);
takeaway(s,'Chỉ hoàn trả tài nguyên sau khi quá trình dọn dẹp tương ứng hoàn tất.',785,28);
}
// 22. Network, storage and ingress: text on the left, one traffic path on the right.
{
const s=base(22,'GIẢI PHÁP ĐỀ XUẤT','Mạng, lưu trữ và truy cập ứng dụng');
text(s,'Mỗi Workspace có vùng mạng riêng, lưu trữ theo hạn mức và đường truy cập ứng dụng riêng.',64,194,1472,46,29,C.muted);
const items=[['Mạng','Mỗi Workspace một IPPool riêng do Calico cấp.\nNetworkPolicy mặc định chặn lưu lượng\ngiữa các Workspace.'],['Lưu trữ','PVC dùng StorageClass được cấp, nằm trong\nhạn mức dung lượng của Workspace.'],['Truy cập ứng dụng','Ingress dùng IngressClass được cấp. Hostname\nnằm trong vùng tên miền riêng của Workspace,\nchứng chỉ TLS do cert-manager cấp.']];
for(let i=0;i<3;i++){const y=286+i*152;text(s,items[i][0],64,y,700,38,27,C.navy,true);text(s,items[i][1],64,y+44,700,100,23,C.ink);}
const user=box(s,'User',856,264,280,76,{fill:C.white,size:25});
rect(s,816,372,720,374,C.white,C.line);
text(s,'WORKSPACE',844,386,300,30,20,C.blue,true);
text(s,'IPPool riêng · NetworkPolicy mặc định',1100,386,412,30,19,C.muted,false,'right');
const ing=box(s,'Ingress',856,440,280,84,{fill:C.light,size:25});
const svc=box(s,'Service',1216,440,280,84,{fill:C.light,size:25});
const pod=box(s,'Pod',1216,606,280,84,{fill:C.navy,stroke:C.navy,color:C.white,size:25});
const pvc=box(s,'PVC',856,606,280,84,{fill:C.light,size:25});
link(s,user,ing,'bottom','top');text(s,'HTTPS',1016,344,120,26,17,C.muted);
link(s,ing,svc);link(s,svc,pod,'bottom','top');link(s,pod,pvc,'left','right');
text(s,'Hostname minh họa',816,758,720,28,20,C.muted);
text(s,'web.acme-solar.apps.cluster-a.tenantry.example',816,788,720,32,22,C.blue,true);
}
// 23. Two authorization planes: portal roles (platform + organization) and Kubernetes roles.
{
const s=base(23,'GIẢI PHÁP ĐỀ XUẤT','Đăng nhập và phân quyền');
text(s,'Keycloak xác thực danh tính một lần. Quyền được cấp ở hai phạm vi tách biệt.',64,194,1472,46,29,C.muted);
text(s,'QUYỀN TRÊN PORTAL',64,258,720,30,22,C.blue,true);
text(s,'Vai trò nền tảng và vai trò trong Organization',64,292,720,30,22,C.muted);
nativeTable(s,[['Vai trò','Phạm vi thao tác'],['Platform Admin','Đăng ký cluster, tạo Organization, cấp ClusterGrant'],['OrgOwner','Quản lý thành viên, group, Workspace và hạn mức'],['OrgAdmin','Như OrgOwner, trừ chuyển quyền sở hữu và xóa Organization'],['OrgMember','Xem Organization và các Workspace được cấp quyền']],64,342,720,[52,78,78,78,78],[220,500],21,-1);
text(s,'QUYỀN TRONG WORKSPACE',816,258,720,30,22,C.blue,true);
text(s,'Vai trò gán cho group theo từng Workspace',816,292,720,30,22,C.muted);
nativeTable(s,[['Vai trò','Phạm vi thao tác'],['TenantOwner','Tạo namespace, quản trị workload và RBAC trong Tenant'],['Developer','Quản lý workload trong namespace được phép'],['Viewer','Xem tài nguyên được cho phép, không đọc Secret']],816,342,720,[52,104,104,104],[230,490],21,-1);
takeaway(s,'Quyền trên Portal và quyền thao tác Kubernetes được cấp riêng, không suy ra từ nhau.',772,27);
}
// 24. Deliberately title only, per user request.
base(24,'','Thực nghiệm và Kết quả',{blank:true});
// 25. Three-column conclusion like template page 20.
{
const s=base(25,'KẾT LUẬN','Kết luận và hướng phát triển');
const items=[['Giải pháp đề xuất','Tenantry cung cấp Workspace\ntrên cluster dùng chung.','Capsule quản lý Tenant.\nTenantry tổ chức cấp môi trường\nvà quyền sử dụng thành dịch vụ.'],['Phạm vi','Người dùng triển khai workload\ntrong tài nguyên và quyền\nđược cấp.','Bên cung cấp vận hành cluster\nvà các thành phần hạ tầng.'],['Hướng phát triển','Mở rộng cấp môi trường\ntrên nhiều cluster.','Bổ sung thành phần cấp phát\ntại từng cluster để hỗ trợ\ntriển khai phân tán.']];
for(let i=0;i<3;i++){const x=64+i*508;text(s,String(i+1).padStart(2,'0'),x,232,444,80,62,C.blue,true);rule(s,x,343,443,C.blue);text(s,items[i][0],x,378,443,76,32,C.navy,true);text(s,items[i][1],x,495,443,142,28);text(s,items[i][2],x,658,443,145,28);}
}
// 26. Closing illustration is a distinct asset.
{
const s=P.slides.add();s.background.fill=C.white;s.speakerNotes.textFrame.setText(notes[26]+'\nHình minh họa: Imagegen.');await img(s,'closing',818,142,716,716);
text(s,'TENANTRY',69,94,1400,46,25,C.blue,true);text(s,'Cảm ơn',64,277,804,153,101,C.navy,true);text(s,'Hỏi đáp và thảo luận',70,463,774,78,42,C.navy);rule(s,70,593,594,C.blue);text(s,'Dịch vụ cung cấp môi trường\nKubernetes với Capsule',70,625,720,109,31,C.muted);
}
// Restore full original speaker notes and asset citations (image facade text reads vary).
for(let i=0;i<P.slides.items.length;i++){const slide=P.slides.items[i];let n=notes[i+1]??'';if(i===23)n='';if(i===0)n+='\nHình: Minh họa hạ tầng được tạo bằng Imagegen.';if(i===25)n+='\nHình: Minh họa Workspace được tạo bằng Imagegen.';const map={3:['kubernetes'],4:['rocket','timer','graduation-cap'],5:['flask-conical','presentation','users','database'],7:['key-round','sliders-horizontal','terminal','refresh-cw'],8:['amazon','google','azure'],9:['server','laptop']};for(const a of map[i+1]??[])n+='\nHình: '+assets[a];slide.speakerNotes.textFrame.setText(n);}
await (await PresentationFile.exportPptx(P)).save(path.join(B,'candidate-v6.pptx'));
await fs.writeFile(path.join(B,'deck.proto.json'),JSON.stringify(P.toProto()));
for(let i=0;i<P.slides.items.length;i++){const sl=P.slides.items[i];const png=await P.export({slide:sl,format:'png',scale:0.833333});await fs.writeFile(path.join(B,'previews',`slide-${String(i+1).padStart(2,'0')}.png`),new Uint8Array(await png.arrayBuffer()));console.log('Rendered',i+1);}
console.log('Wrote candidate with',P.slides.items.length,'slides');
