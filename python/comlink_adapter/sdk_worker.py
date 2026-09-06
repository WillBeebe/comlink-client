"""One-turn ADK or Claude SDK worker; SDK dependencies are externally installed."""
import asyncio
import json
import logging
import os
from pathlib import Path
import sys

logging.disable(1000)
wire=os.fdopen(os.dup(1),'w',buffering=1)
null=os.open(os.devnull,os.O_WRONLY)
os.dup2(null,1);os.dup2(null,2);os.close(null)

async def adk(prompt,instruction,model):
    from google.adk.agents import LlmAgent
    from google.adk.models.base_llm import BaseLlm
    from google.adk.models.llm_response import LlmResponse
    from google.adk.runners import Runner
    from google.adk.sessions import InMemorySessionService
    from google.adk.agents.run_config import RunConfig
    from google.genai import types
    import urllib.request
    class ComlinkModel(BaseLlm):
        async def generate_content_async(self,llm_request,stream=False):
            if llm_request.tools_dict:raise RuntimeError('unexpected ADK tools')
            messages=[{'role':'system','content':instruction}]
            for content in llm_request.contents:
                if any(p.function_call or p.function_response for p in content.parts):raise RuntimeError('non-text ADK content')
                messages.append({'role':'assistant' if content.role=='model' else 'user','content':''.join(p.text or '' for p in content.parts)})
            req=urllib.request.Request(os.environ['COMLINK_GATE_URL']+'/chat/completions',data=json.dumps({'model':model,'messages':messages,'stream':True}).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+os.environ['COMLINK_GATE_TOKEN']})
            # This synchronous request is isolated in a killable worker, not the MCP reader.
            with urllib.request.urlopen(req,timeout=35) as response:
                raw=response.read(262145)
            if len(raw)>262144:raise RuntimeError('response too large')
            text=[]
            for line in raw.decode().splitlines():
                if line.startswith('data: ') and line!='data: [DONE]':
                    chunk=json.loads(line[6:]);text.append(chunk['choices'][0]['delta'].get('content',''))
            yield LlmResponse(content=types.Content(role='model',parts=[types.Part(text=''.join(text))]))
    sessions=InMemorySessionService()
    session=await sessions.create_session(app_name='comlink',user_id='endpoint')
    agent=LlmAgent(name='comlink',model=ComlinkModel(model=model),instruction=instruction,tools=[])
    runner=Runner(app_name='comlink',agent=agent,session_service=sessions)
    try:
        result=None
        async for event in runner.run_async(user_id='endpoint',session_id=session.id,new_message=types.Content(role='user',parts=[types.Part(text=prompt)]),run_config=RunConfig(max_llm_calls=1)):
            if event.is_final_response() and event.content:
                result=''.join(p.text or '' for p in event.content.parts)
        if not result:raise RuntimeError('no ADK response')
        return result
    finally:
        await sessions.delete_session(app_name='comlink',user_id='endpoint',session_id=session.id)
        await runner.close()

async def claude(prompt,instruction,model):
    import claude_agent_sdk
    import subprocess
    from claude_agent_sdk import query,ClaudeAgentOptions,ResultMessage,InMemorySessionStore
    cli=Path(claude_agent_sdk.__file__).parent/'_bundled'/'claude'
    if subprocess.check_output([str(cli),'--version'],timeout=10).decode().strip()!='2.1.259 (Claude Code)':
        raise RuntimeError('unverified bundled Claude runtime')
    options=ClaudeAgentOptions(cli_path=str(cli),model=model,system_prompt=instruction,tools=[],allowed_tools=[],
        mcp_servers={},strict_mcp_config=True,permission_mode='dontAsk',setting_sources=[],skills=[],
        max_turns=1,enable_file_checkpointing=False,session_store=InMemorySessionStore(),
        extra_args={'no-session-persistence':None,'debug-file':os.devnull},
        env={'ANTHROPIC_BASE_URL':os.environ['COMLINK_GATE_URL'].removesuffix('/v1'),
            'ANTHROPIC_API_KEY':os.environ['COMLINK_GATE_TOKEN']},cwd=os.getcwd(),
        thinking={'type':'disabled'},max_buffer_size=262144)
    result=None
    async for message in query(prompt=prompt,options=options):
        if isinstance(message,ResultMessage):
            if message.is_error:raise RuntimeError('Claude SDK turn failed')
            result=message.result
    if not result:raise RuntimeError('no Claude SDK response')
    return result

async def main():
    prompt=sys.stdin.buffer.read(524289)
    if len(prompt)>524288:raise RuntimeError('input too large')
    instruction=Path('instructions.md').read_text()
    fn=adk if sys.argv[1]=='adk' else claude
    result=await fn(prompt.decode(),instruction,sys.argv[2])
    if len(result.encode())>100000:raise RuntimeError('output too large')
    wire.write(result+'\n')

if __name__=='__main__':
    try:asyncio.run(main())
    except BaseException:os._exit(1)
    os._exit(0)
