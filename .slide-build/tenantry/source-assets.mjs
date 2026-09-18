import fs from 'node:fs/promises';
import sharp from 'sharp';
const dir=new URL('./assets/',import.meta.url);
const sources={kubernetes:'https://raw.githubusercontent.com/cncf/artwork/main/projects/kubernetes/icon/color/kubernetes-icon-color.svg',amazon:'https://raw.githubusercontent.com/devicons/devicon/master/icons/amazonwebservices/amazonwebservices-original-wordmark.svg',google:'https://raw.githubusercontent.com/devicons/devicon/master/icons/googlecloud/googlecloud-original.svg',azure:'https://raw.githubusercontent.com/devicons/devicon/master/icons/azure/azure-original.svg'};
for(const name of ['server','laptop','rocket','database','graduation-cap','cpu','key-round','network','sliders-horizontal','terminal','refresh-cw','users','globe','hard-drive','shield-check'])sources[name]=`https://raw.githubusercontent.com/lucide-icons/lucide/main/icons/${name}.svg`;
for(const [name,url]of Object.entries(sources)){const r=await fetch(url);if(!r.ok)throw Error(name+' '+r.status);const b=Buffer.from(await r.arrayBuffer());await fs.writeFile(new URL(name+'.svg',dir),b);await sharp(b,{density:288}).resize(320,320,{fit:'inside'}).png().toFile(new URL(name+'.png',dir).pathname);console.log(name);}
await fs.writeFile(new URL('sources.json',dir),JSON.stringify(sources,null,2));
