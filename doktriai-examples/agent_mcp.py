import json
import urllib.request

class DoktriaiAgentBridge:
    def __init__(self, api_url="http://localhost:18080", actor="Agent-Claude", role="admin"):
        self.api_url = f"{api_url}/api/mcp"
        self.actor = actor
        self.role = role

    def call_mcp(self, method, params=None):
        payload = {
            "jsonrpc": "2.0",
            "id": 1,
            "method": method
        }
        if params:
            payload["params"] = params

        req = urllib.request.Request(
            self.api_url,
            data=json.dumps(payload).encode("utf-8"),
            headers={
                "Content-Type": "application/json",
                "X-Doktri-Actor": self.actor,
                "X-Doktri-Role": self.role
            },
            method="POST"
        )
        try:
            with urllib.request.urlopen(req) as res:
                response = json.loads(res.read().decode("utf-8"))
                return response
        except Exception as e:
            return {"error": str(e)}

if __name__ == "__main__":
    print("[DOKTRIAI] Initializing Agent MCP Connection Bridge...")
    bridge = DoktriaiAgentBridge()
    
    # 1. List exposed automation tools
    print("\n[STEP 1] Listing exposed tools:")
    tools = bridge.call_mcp("tools/list")
    print(json.dumps(tools, indent=2))
    
    # 2. Deploy a safe workload via tool call
    print("\n[STEP 2] Calling tool: deploy_workload")
    deploy_args = {
        "name": "deploy_workload",
        "arguments": {
            "name": "agent-nginx",
            "image": "nginx:alpine",
            "replicas": 2,
            "port": 8081,
            "containerPort": 80,
            "runtime": "docker"
        }
    }
    result = bridge.call_mcp("tools/call", deploy_args)
    print(json.dumps(result, indent=2))
