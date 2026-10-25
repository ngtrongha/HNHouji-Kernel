// A small text-only Markdown renderer. Provider HTML, links and code never
// become executable markup. No innerHTML, HTML parser or external assets.
function inline(parent:HTMLElement,value:string) {
  const parts=value.split(/(\*\*[^*]+\*\*|`[^`]+`)/g);
  for(const part of parts) { if(part.startsWith('**')&&part.endsWith('**')){const strong=document.createElement('strong');strong.textContent=part.slice(2,-2);parent.append(strong);}else if(part.startsWith('`')&&part.endsWith('`')){const code=document.createElement('code');code.textContent=part.slice(1,-1);parent.append(code);}else parent.append(document.createTextNode(part)); }
}
export function markdown(parent:HTMLElement,value:string) {
  const lines=String(value).replace(/\r/g,'').split('\n');let list:HTMLElement|undefined;let code:string[]|undefined;let table:HTMLTableElement|undefined;
  for(const [index,line] of lines.entries()) {
    if(index>=2000){const details=document.createElement('details');const summary=document.createElement('summary');summary.textContent='Xem phần còn lại';const pre=document.createElement('pre');pre.textContent=lines.slice(index).join('\n');details.append(summary,pre);parent.append(details);break;}
    if(line.trim().startsWith('```')) {if(code){const pre=document.createElement('pre');pre.textContent=code.join('\n');parent.append(pre);code=undefined;}else code=[];list=undefined;table=undefined;continue;}
    if(code){code.push(line);continue;}
    if(!line.trim()){list=undefined;table=undefined;continue;}
    if(/^\s*\|?\s*:?-{3,}/.test(line)&&line.includes('|'))continue;
    if(line.trim().startsWith('|')&&line.trim().endsWith('|')) {if(!table){table=document.createElement('table');const wrap=document.createElement('div');wrap.className='answer-table';wrap.append(table);parent.append(wrap);}const row=table.insertRow();for(const cell of line.trim().slice(1,-1).split('|'))inline(row.insertCell(),cell.trim());list=undefined;continue;}
    table=undefined;const heading=/^#{1,6}\s+(.+)$/.exec(line);
    if(heading){const h=document.createElement('h4');inline(h,heading[1]);parent.append(h);list=undefined;continue;}
    const item=/^\s*(?:[-*+]\s+|\d+[.)]\s+)(.+)$/.exec(line);
    if(item){const ordered=/^\s*\d/.test(line);const tag=ordered?'OL':'UL';if(!list||list.tagName!==tag){list=document.createElement(ordered?'ol':'ul');parent.append(list);}const li=document.createElement('li');inline(li,item[1]);list.append(li);continue;}
    list=undefined;const p=document.createElement('p');inline(p,line);parent.append(p);
  }
  if(code){const pre=document.createElement('pre');pre.textContent=code.join('\n');parent.append(pre);}
}
