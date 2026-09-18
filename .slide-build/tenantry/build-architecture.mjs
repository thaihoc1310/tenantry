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
function nativeTable(s,vals,x,y,width,heights,cols,font=24){const height=heights.reduce((a,b)=>a+b,0);const t=s.tables.add({rows:vals.length,columns:vals[0].length,left:x*S,top:y*S,width:width*S,height:height*S,columnWidths:cols.map(v=>v*S),values:vals});t.borders.assign({fill:C.line,width:1,style:'solid'});for(let r=0;r<vals.length;r++){t.rows[r].height=heights[r]*S;for(let c=0;c<vals[r].length;c++){const cell=t.getCell(r,c);cell.fill=r===0?C.navy:r%2?C.light:C.white;cell.text.style={typeface:F,fontSize:font*S,color:r===0?C.white:C.ink,bold:r===0,alignment:'left',verticalAlignment:'middle',autoFit:'none',wrap:'square',insets:{left:15*S,right:15*S,top:10*S,bottom:10*S}};}}return t;}
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
// 04. Needs connect to one environment; examples are not market survey findings.
{
const s=base(4,'GIỚI THIỆU','Nhu cầu truy cập môi trường Kubernetes');
text(s,'Người dùng cần truy cập Kubernetes vì nhiều mục đích khác nhau:',64,199,1460,53,30,C.muted);
const central=box(s,'Môi trường\nKubernetes',651,382,315,150,{fill:C.navy,color:C.white,size:36});
const a=box(s,'Triển khai và vận hành\nsản phẩm hoặc dịch vụ',64,323,487,130,{fill:C.white,stroke:'none',size:31});await img(s,'rocket',271,255,64,64);
const b=box(s,'Xử lý dữ liệu và thực hiện\ncác tác vụ tính toán',1064,323,472,130,{fill:C.white,stroke:'none',size:31});await img(s,'database',1263,255,64,64);
const c=box(s,'Học Kubernetes và thử nghiệm với các API',467,616,690,70,{fill:C.white,stroke:'none',size:30});await img(s,'graduation-cap',350,615,72,72);
link(s,a,central);link(s,b,central,'left','right');link(s,c,central,'top','bottom');
takeaway(s,'Điểm chung: một môi trường dùng được, có tài nguyên và quyền truy cập.');
}
// 05. Preparation barrier is visible in a single process.
{
const s=base(5,'GIỚI THIỆU','Rào cản khi tiếp cận môi trường Kubernetes',{size:46});
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
// 06. Four goals, with one coherent environment concept.
{
const s=base(6,'GIỚI THIỆU','Mục tiêu đồ án');
const goals=[['Tự phục vụ','Yêu cầu quyền truy cập Kubernetes trên hạ tầng sẵn có.','key-round'],['Cấu hình theo nhu cầu','Cấp môi trường với tài nguyên và quyền truy cập có thể cấu hình.','sliders-horizontal'],['Công cụ tiêu chuẩn','Hỗ trợ công cụ Kubernetes và workload do người dùng tự chọn.','terminal'],['Quản lý tập trung','Cấp phát, cập nhật và dọn dẹp môi trường qua một nền tảng.','refresh-cw']];
for(let i=0;i<4;i++){await img(s,goals[i][2],65,211+i*123,65,65);text(s,goals[i][0],167,211+i*123,520,47,31,C.navy,true);text(s,goals[i][1],167,258+i*123,1333,54,29,C.ink);}
takeaway(s,'Người dùng yêu cầu môi trường và nhận quyền truy cập, nền tảng vận hành cluster.',764,28);
}
// 07. What the services do, effective for, limitations tied to this problem.
{
const s=base(7,'CÁC GIẢI PHÁP HIỆN CÓ','Dịch vụ Managed Kubernetes');
for(const [name,label,x]of [['amazon','Amazon EKS',64],['google','Google GKE',525],['azure','Microsoft AKS',1005]]){await img(s,name,x,194,73,70);text(s,label,x+94,211,370,47,31,C.navy,true);}text(s,'…',1465,211,65,48,34,C.muted);
text(s,'Cung cấp cluster để chạy workload. Nhà cung cấp cài đặt và vận hành control plane.',64,294,1472,88,30,C.ink);
rule(s,64,387,1472);
text(s,'Phù hợp với',64,419,567,49,32,C.blue,true);text(s,'Hạn chế',738,419,796,49,32,C.blue,true);
label(s,'Production trên cloud','Cần mở rộng và tích hợp dịch vụ cloud.',64,492,570,{size:29,bodySize:28,gap:43});
label(s,'Cluster độc lập','Cần cấu hình và vòng đời riêng.',64,627,570,{size:29,bodySize:28,gap:43});
const list=[['Tài nguyên và chi phí','Cluster riêng có thể dư tài nguyên, tăng chi phí cho nhu cầu nhỏ.'],['Thời gian chuẩn bị','Cluster mới cần thời gian và công sức khởi tạo, cấu hình.'],['Môi trường dùng chung','Cần bổ sung cấp môi trường, hạn mức và quyền truy cập cho từng người dùng.']];
for(let i=0;i<3;i++){text(s,list[i][0],738,491+i*104,790,38,27,C.navy,true);text(s,list[i][1],738,530+i*104,790,72,26,C.ink);}
}
// 08. Two groups retained within one slide.
{
const s=base(8,'CÁC GIẢI PHÁP HIỆN CÓ','Tự triển khai Kubernetes');
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
// 09. Smaller scope: division inside an already shared cluster.
{
const s=base(9,'CÁC GIẢI PHÁP HIỆN CÓ','Chia sẻ cluster bằng namespace');
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
// 10. Capsule grouping and tenant-owner namespace self-service.
{
const s=base(10,'GIẢI PHÁP ĐỀ XUẤT','Capsule: Quản lý namespace theo Tenant',{size:46});
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
// 11. Product definition before internal names and stack.
{
const s=base(11,'GIẢI PHÁP ĐỀ XUẤT','Tenantry: Dịch vụ cung cấp môi trường Kubernetes',{size:42});
text(s,'Người dùng tự yêu cầu và quản lý môi trường trên hạ tầng dùng chung qua Portal.',64,191,1472,74,30,C.ink);
const user=box(s,'Người dùng',64,341,231,88,{fill:C.white,size:29});const portal=box(s,'Portal\nTenantry',391,322,247,126,{fill:C.navy,color:C.white,size:31});
const ws=rect(s,770,288,766,268,C.light,C.blue);text(s,'WORKSPACE',808,308,687,51,35,C.navy,true);text(s,'Một Workspace tương ứng một Capsule Tenant',808,365,687,66,26,C.blue,true);
box(s,'namespace',811,449,298,67,{fill:C.white,size:25});box(s,'namespace',1169,449,325,67,{fill:C.white,size:25});link(s,user,portal);link(s,portal,ws);
const caps=[['Không gian sử dụng','Tạo namespace,\nchạy workload'],['Tài nguyên và cấu hình','Hạn mức, mạng\nvà lưu trữ'],['Quyền truy cập','Cấp quyền cho\nngười cùng dùng'],['Công cụ kết nối','kubeconfig và\ngiao diện web']];
for(let i=0;i<4;i++){const x=64+i*384;rule(s,x,606,334,C.blue);label(s,caps[i][0],caps[i][1],x,625,330,{size:25,bodySize:24,gap:45});}
takeaway(s,'Người dùng chọn công việc cần chạy. Bên cung cấp vận hành cluster bên dưới.',770,28);
}
// 12. Full user journey, not the individual create-form steps.
{
const s=base(12,'GIẢI PHÁP ĐỀ XUẤT','Luồng sử dụng Tenantry');
text(s,'Portal là giao diện web để yêu cầu và quản lý môi trường.',64,199,1436,59,31,C.muted);
const items=[['Đăng nhập','Xem môi trường và\ntài nguyên được phép'],['Tạo Workspace','Chọn tài nguyên,\nmạng và nhóm truy cập'],['Gửi yêu cầu','Xem lại lựa chọn,\ntheo dõi cấp phát'],['Nhận môi trường','Tải kubeconfig hoặc\nmở giao diện web'],['Sử dụng','Tạo namespace và\ntriển khai workload']];
let prev;for(let i=0;i<5;i++){const x=64+i*300;text(s,String(i+1).padStart(2,'0'),x,335,257,98,68,C.blue,true);const b=box(s,items[i][0],x,466,267,95,{fill:i===3?C.navy:C.light,color:i===3?C.white:C.navy,size:26});if(prev)link(s,prev,b);prev=b;text(s,items[i][1],x,591,267,127,26);}
takeaway(s,'Người có quyền quản lý có thể điều chỉnh hạn mức hoặc xóa Workspace qua Portal.',772,27);
}
// 13. Organization manager assigns resources and group access to Workspaces.
{
const s=base(13,'GIẢI PHÁP ĐỀ XUẤT','Tổ chức người dùng và phân bổ tài nguyên',{size:46});
text(s,'Organization quản lý thành viên và các Workspace của một cá nhân hoặc nhóm.',64,195,1472,70,30);
const admin=box(s,'Quản trị viên nền tảng',555,272,490,76,{fill:C.white,size:29});
const org=rect(s,64,410,1472,351,C.white,C.line);
text(s,'ORGANIZATION',95,430,421,43,27,C.blue,true);
text(s,'Hạn mức: 8 CPU / 16 GiB',573,430,561,43,27,C.navy,true);
text(s,'Còn lại: 2 CPU / 4 GiB',1170,434,340,40,24,C.muted);
rule(s,95,484,1410,C.line);
link(s,admin,org,'bottom','top');
text(s,'Cấp hạn mức và lựa chọn hạ tầng',834,366,684,37,24,C.muted);
const manager=box(s,'Người quản lý Organization',105,583,445,90,{fill:C.navy,color:C.white,size:28});
const a=box(s,'Workspace A: 2 CPU / 4 GiB\nNhóm được gán: Nhóm A',977,519,520,95,{fill:C.light,size:26});
const b=box(s,'Workspace B: 4 CPU / 8 GiB\nNhóm được gán: Nhóm B',977,643,520,95,{fill:C.light,size:26});
link(s,manager,a,'right','left');
link(s,manager,b,'right','left');
text(s,'Phân bổ tài nguyên\nvà gán nhóm truy cập',581,495,368,78,26,C.blue,true);
text(s,'Ví dụ phân bổ trên một cluster',95,703,650,36,23,C.muted);
takeaway(s,'Tổng phân bổ cho các Workspace phải nằm trong hạn mức Organization trên cluster đó.',789,26);
}
// 14. Integrated component architecture with shared API and identity services.
{
const s=base(14,'GIẢI PHÁP ĐỀ XUẤT','Kiến trúc tổng thể Tenantry');
rect(s,48,192,1504,608,C.white,C.line);
const portal=box(s,'Portal\nReact / TypeScript',80,235,290,100,{fill:C.light,size:28});
const api=box(s,'Platform API\nGo',575,235,345,100,{fill:C.light,size:28});
const db=box(s,'DB\nPostgreSQL',1190,235,340,100,{fill:C.light,size:28});
const kc=box(s,'Keycloak',80,463,290,100,{fill:'#EAF6F9',stroke:C.cyan,color:C.cyan,size:31});
const kapi=box(s,'Kubernetes API',650,463,325,100,{fill:C.navy,stroke:C.navy,color:C.white,size:30});
const worker=box(s,'Worker cấp phát\nGo',1190,463,340,100,{fill:C.light,size:28});
const cli=box(s,'kubectl / Headlamp',80,680,290,100,{fill:C.white,size:26});
const proxy=box(s,'Capsule Proxy',440,680,260,100,{fill:C.white,size:27});
const controller=box(s,'Controller runtime\nGo',850,680,330,100,{fill:C.light,size:28});
const tenant=box(s,'Tenant và cấu hình\nCapsule / Calico / RBAC',1240,675,290,110,{fill:C.light,size:23});
link(s,portal,api);
link(s,api,db,'right','left',C.blue,false,true);
link(s,worker,db,'top','bottom',C.blue,false,true);
link(s,worker,kapi,'left','right',C.blue,false,true);
link(s,controller,kapi,'top','bottom',C.blue,false,true);
link(s,controller,tenant);
link(s,cli,proxy);
link(s,proxy,kapi,'top','left');
// Dashed authentication associations use an orthogonal shared identity branch.
function authPath(points){for(let j=1;j<points.length;j++){const a=points[j-1],b=points[j];s.shapes.add({geometry:'line',position:pos(Math.min(a[0],b[0]),Math.min(a[1],b[1]),Math.abs(a[0]-b[0]),Math.abs(a[1]-b[1])),fill:'none',line:{fill:C.cyan,width:1.8*S,style:'dashed'}});}}
link(s,portal,kc,'bottom','top',C.cyan,true);
link(s,cli,kc,'top','bottom',C.cyan,true);
const authAnchor=rect(s,415,512.95,0.1,0.1,'none','none');
link(s,authAnchor,kc,'left','right',C.cyan,true);
authPath([[415,513],[415,382],[747.5,382],[747.5,335]]);
authPath([[415,414],[812.5,414],[812.5,463]]);
}
// 15. Ordered create/delete lifecycle.
{
const s=base(15,'GIẢI PHÁP ĐỀ XUẤT','Cấp phát và thu hồi Workspace');
text(s,'TẠO MÔI TRƯỜNG',64,213,1472,44,26,C.blue,true);
const create=[['01  Kiểm tra','Quyền, hạn mức và\ntài nguyên khả dụng'],['02  Ghi nhận','Phân bổ tài nguyên\nvà yêu cầu cấp phát'],['03  Cấp phát','Worker gửi yêu cầu.\nController tạo Tenant\nvà cấu hình môi trường.'],['04  Sẵn sàng','Cập nhật trạng thái.\nCấp thông tin truy cập\nkhi môi trường sẵn sàng.']];
let prev;for(let i=0;i<4;i++){const x=64+i*384;const sh=box(s,create[i][0],x,300,334,83,{fill:i===3?C.navy:C.light,color:i===3?C.white:C.navy,size:28});if(prev)link(s,prev,sh);prev=sh;text(s,create[i][1],x,412,334,118,27);}
text(s,'THU HỒI MÔI TRƯỜNG',64,564,1472,44,26,C.blue,true);
const a=box(s,'Thu hồi quyền truy cập',64,639,409,89,{fill:C.white,size:27});const b=box(s,'Xóa namespace và\ntài nguyên liên quan',599,639,426,89,{fill:C.white,size:26});const c=box(s,'Hoàn trả tài nguyên\nđã phân bổ',1152,639,384,89,{fill:C.light,size:26});link(s,a,b);link(s,b,c);
takeaway(s,'Chỉ hoàn trả tài nguyên sau khi quá trình dọn dẹp tương ứng hoàn tất.',785,28);
}
// 16. Native comparison table and explicit shared control plane.
{
const s=base(16,'GIẢI PHÁP ĐỀ XUẤT','Tài nguyên trên node dùng chung và node riêng',{size:44});
text(s,'Workspace chọn phương án compute trong phạm vi Organization được cấp.',64,196,1472,66,29);
const cp=box(s,'Control plane dùng chung',528,274,551,66,{fill:C.navy,color:C.white,size:27});
const shared=box(s,'Shared\nNhóm node cho nhiều Organization',95,383,613,87,{fill:C.light,size:27});const ded=box(s,'Dedicated\nNode riêng cho một Organization',891,383,613,87,{fill:C.light,size:27});link(s,cp,shared,'bottom','top');link(s,cp,ded,'bottom','top');
nativeTable(s,[['','Node dùng chung (Shared)','Node riêng (Dedicated)'],['Ai dùng hạ tầng','Nhiều Organization dùng chung nhóm node','Node riêng cho một Organization'],['Cấp tài nguyên','Hạn mức trên nhóm node chung','Giữ chỗ CPU, bộ nhớ và số Pod'],['Vị trí Workspace','Trong nhóm node được phép','Một node thuộc Organization']],64,490,1472,[54,67,71,67],[278,597,597],24);
takeaway(s,'Một node Dedicated có thể phục vụ nhiều Workspace của cùng Organization.',785,28);
}
// 17. Application traffic separated from control/API access.
{
const s=base(17,'GIẢI PHÁP ĐỀ XUẤT','Mạng, lưu trữ và truy cập ứng dụng');
label(s,'Mạng cho workload','Calico cấp dải địa chỉ Pod.\nChính sách mạng giới hạn kết nối\ngiữa các môi trường.',64,212,655,{bodySize:28,gap:50});
label(s,'Lưu trữ dữ liệu','Yêu cầu dung lượng qua PVC,\ntrong hạn mức và loại lưu trữ được cấp.',64,419,655,{bodySize:28,gap:50});
label(s,'Truy cập ứng dụng','Khai báo Ingress để truy cập qua HTTPS\ntrong vùng tên miền của Workspace.',64,623,655,{bodySize:28,gap:50});
rect(s,845,357,691,365,C.light,C.blue);text(s,'WORKSPACE',1165,375,330,44,27,C.blue,true);
const browser=box(s,'Trình duyệt',894,240,220,76,{fill:C.white,size:25});const ingress=box(s,'Ingress',894,445,220,76,{fill:C.white,size:25});link(s,browser,ingress,'bottom','top');text(s,'HTTPS',1060,327,170,33,21,C.blue,true);
const service=box(s,'Service',1250,445,229,76,{fill:C.white,size:25});const pod=box(s,'Pod',1250,603,229,76,{fill:C.navy,color:C.white,size:27});link(s,ingress,service);link(s,service,pod,'bottom','top');
const pvc=box(s,'PVC / lưu trữ',894,603,270,76,{fill:C.white,size:25});link(s,pod,pvc,'left','right');text(s,'Ranh giới chính sách mạng',894,550,335,50,22,C.muted);
text(s,'Hostname minh họa',845,755,681,33,22,C.muted);text(s,'web.acme-solar.apps.cluster-a.tenantry.example',845,793,692,40,22,C.blue,true);
}
// 18. Separate scopes, with editable role table.
{
const s=base(18,'GIẢI PHÁP ĐỀ XUẤT','Đăng nhập và phân quyền');
const identity=box(s,'Keycloak xác thực danh tính',505,204,610,78,{fill:C.navy,color:C.white,size:29});
const portal=box(s,'Quyền trên Portal',64,359,573,71,{fill:C.light,size:29});const workspace=box(s,'Quyền trong Workspace',790,359,746,71,{fill:C.light,size:29});link(s,identity,portal,'bottom','top');link(s,identity,workspace,'bottom','top');
text(s,'Vai trò trong Organization quyết định\nviệc quản lý thành viên, phân bổ\ntài nguyên và tạo Workspace.',64,482,570,148,30);
text(s,'Nhóm thành viên được gán vai trò theo Workspace.',790,455,746,71,26,C.muted);
nativeTable(s,[['Vai trò','Phạm vi thao tác'],['TenantOwner','Quản lý namespace và tài nguyên trong Tenant'],['Developer','Quản lý workload trong namespace được phép'],['Viewer','Xem tài nguyên được cho phép']],790,515,746,[52,65,65,65],[215,531],23);
takeaway(s,'Quyền quản lý Organization và quyền thao tác Kubernetes được cấp riêng.',792,28);
}
// 19. Deliberately title only, per user request.
base(19,'','Thực nghiệm và Kết quả',{blank:true});
// 20. Three-column conclusion like template page 20.
{
const s=base(20,'KẾT LUẬN','Kết luận và hướng phát triển');
const items=[['Giải pháp đề xuất','Tenantry cung cấp Workspace\ntrên cluster dùng chung.','Capsule quản lý Tenant.\nTenantry tổ chức cấp môi trường\nvà quyền sử dụng thành dịch vụ.'],['Phạm vi','Người dùng triển khai workload\ntrong tài nguyên và quyền\nđược cấp.','Bên cung cấp vận hành cluster\nvà các thành phần hạ tầng.'],['Hướng phát triển','Mở rộng cấp môi trường\ntrên nhiều cluster.','Bổ sung thành phần cấp phát\ntại từng cluster để hỗ trợ\ntriển khai phân tán.']];
for(let i=0;i<3;i++){const x=64+i*508;text(s,String(i+1).padStart(2,'0'),x,232,444,80,62,C.blue,true);rule(s,x,343,443,C.blue);text(s,items[i][0],x,378,443,76,32,C.navy,true);text(s,items[i][1],x,495,443,142,28);text(s,items[i][2],x,658,443,145,28);}
}
// 21. Closing illustration is a distinct asset.
{
const s=P.slides.add();s.background.fill=C.white;s.speakerNotes.textFrame.setText(notes[21]+'\nHình minh họa: Imagegen.');await img(s,'closing',818,142,716,716);
text(s,'TENANTRY',69,94,1400,46,25,C.blue,true);text(s,'Cảm ơn',64,277,804,153,101,C.navy,true);text(s,'Hỏi đáp và thảo luận',70,463,774,78,42,C.navy);rule(s,70,593,594,C.blue);text(s,'Dịch vụ cung cấp môi trường\nKubernetes với Capsule',70,625,720,109,31,C.muted);
}
// Restore full original speaker notes and asset citations (image facade text reads vary).
for(let i=0;i<P.slides.items.length;i++){const slide=P.slides.items[i];let n=notes[i+1]??'';if(i===18)n='';if(i===0)n+='\nHình: Minh họa hạ tầng được tạo bằng Imagegen.';if(i===20)n+='\nHình: Minh họa Workspace được tạo bằng Imagegen.';const map={3:['kubernetes'],4:['rocket','database','graduation-cap'],6:['key-round','sliders-horizontal','terminal','refresh-cw'],7:['amazon','google','azure'],8:['server','laptop']};for(const a of map[i+1]??[])n+='\nHình: '+assets[a];slide.speakerNotes.textFrame.setText(n);}
await (await PresentationFile.exportPptx(P)).save(path.join(B,'architecture-review/generated.pptx'));
await fs.writeFile(path.join(B,'architecture-review/deck.proto.json'),JSON.stringify(P.toProto()));
for(let i=13;i<14;i++){const sl=P.slides.items[i];const png=await P.export({slide:sl,format:'png',scale:0.833333});await fs.writeFile(path.join(B,'architecture-review',`slide-${String(i+1).padStart(2,'0')}.png`),new Uint8Array(await png.arrayBuffer()));console.log('Rendered',i+1);}
console.log('Wrote candidate with',P.slides.items.length,'slides');
