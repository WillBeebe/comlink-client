// One isolated, in-memory OpenClaw agent-core turn. No gateway or session store.
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const output = process.stdout.write.bind(process.stdout);
process.stdout.write = () => true;
process.stderr.write = () => true;
for (const name of ['log','info','warn','error','debug']) console[name] = () => {};
try {
  const versionOnly = process.argv[2] === '--version';
  const packageRoot = process.argv[versionOnly ? 3 : 2];
  const metadata = JSON.parse(fs.readFileSync(path.join(packageRoot,'package.json'),'utf8'));
  const [major,minor,patch] = process.versions.node.split('.').map(Number);
  const supportedNode = (major===22 && (minor>22 || minor===22 && patch>=3)) ||
    (major===24 && minor>=15) || (major===25 && minor>=9) || major>=26;
  if (!supportedNode || metadata.name!=='openclaw' || metadata.version!=='2026.9.2') throw Error();
  if (versionOnly) { output(metadata.version+'\n'); process.exit(0); }
  const load = name => import(pathToFileURL(path.join(packageRoot,'dist/plugin-sdk',name+'.js')).href);
  const { Agent } = await load('agent-core');
  const { createAssistantMessageEventStream } = await load('llm');
  const input=[];let bytes=0;
  for await (const chunk of process.stdin) {
    bytes+=chunk.length;
    if (bytes>524288) throw Error();
    input.push(chunk);
  }
  const prompt=Buffer.concat(input).toString('utf8');
  const model={id:process.argv[3],name:'Comlink',api:'openai-completions',provider:'comlink',
    baseUrl:process.env.COMLINK_GATE_URL,reasoning:false,input:['text'],contextWindow:64000,maxTokens:512,
    cost:{input:0,output:0,cacheRead:0,cacheWrite:0}};
  let requests=0;
  const streamFn = (_model,context,options) => {
    const stream=createAssistantMessageEventStream();
    const message={role:'assistant',content:[],api:model.api,provider:model.provider,model:model.id,
      usage:{input:0,output:0,cacheRead:0,cacheWrite:0,totalTokens:0,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}},
      stopReason:'stop',timestamp:Date.now()};
    void (async()=>{
      try {
        if (++requests!==1 || context.tools?.length) throw Error();
        const messages=[{role:'system',content:context.systemPrompt}];
        for (const item of context.messages) {
          if (!['user','assistant'].includes(item.role)) throw Error();
          const content=typeof item.content==='string' ? item.content : item.content.map(part=>{
            if (part.type!=='text') throw Error();return part.text;
          }).join('');
          messages.push({role:item.role,content});
        }
        const response=await fetch(model.baseUrl+'/chat/completions',{method:'POST',redirect:'error',
          headers:{'Content-Type':'application/json',Authorization:'Bearer '+process.env.COMLINK_GATE_TOKEN},
          body:JSON.stringify({model:model.id,messages,stream:true,tools:[]}),signal:options?.signal});
        if (!response.ok || !response.body) throw Error();
        const chunks=[];let size=0;
        for await (const chunk of response.body) {
          size+=chunk.length;if(size>262144) throw Error();chunks.push(Buffer.from(chunk));
        }
        const wire=Buffer.concat(chunks).toString('utf8');
        let text='';
        for (const line of wire.split('\n')) {
          if (!line.startsWith('data: ') || line==='data: [DONE]') continue;
          const value=JSON.parse(line.slice(6));
          for(const choice of value.choices??[]) {
            if(choice.delta?.tool_calls || choice.delta?.function_call) throw Error();
            text+=choice.delta?.content??'';
          }
        }
        if (!text || Buffer.byteLength(text)>100000) throw Error();
        message.content=[{type:'text',text}];stream.push({type:'done',reason:'stop',message});stream.end(message);
      } catch {
        message.stopReason='error';message.errorMessage='Comlink inference failed';
        stream.push({type:'error',reason:'error',error:message});stream.end(message);
      }
    })();
    return stream;
  };
  const agent=new Agent({initialState:{model,systemPrompt:fs.readFileSync('instructions.md','utf8'),tools:[],messages:[],thinkingLevel:'off'},streamFn});
  process.once('SIGTERM',()=>{agent.abort();process.exit(1);});
  await agent.prompt(prompt);
  const last=agent.state.messages.at(-1);
  if (last?.role!=='assistant' || last.stopReason!=='stop' || last.content.some(p=>p.type!=='text')) throw Error();
  const result=last.content.map(p=>p.text).join('');
  agent.reset();
  output(result,()=>process.exit(0));
} catch { process.exit(1); }
