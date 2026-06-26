package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/praful224/doktriai/doktriai-cli"
	"github.com/praful224/doktriai/doktriai-core"
)

func main() {
	apiURL := flag.String("api", "http://localhost:18080", "doktriai-api base URL")
	role := flag.String("role", "admin", "Doktri role header (admin|operator|viewer)")
	actor := flag.String("actor", "doktriai-cli", "Actor name (your identity)")
	flag.Parse()

	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}

	client := cli.NewClient(*apiURL, *role, *actor)
	if tokenVal := os.Getenv("DOKTRIAI_TOKEN"); tokenVal != "" {
		client = client.WithToken(tokenVal)
	}
	command := flag.Arg(0)
	var err error

	switch command {

	// ── Core workload commands ─────────────────────────────────────────────
	case "health":
		err = client.Call("GET", "/api/health", nil)
	case "list", "status":
		err = client.Call("GET", "/api/workloads", nil)
	case "get":
		if flag.NArg() < 2 {
			err = fmt.Errorf("get requires workload name")
			break
		}
		err = client.Call("GET", "/api/workloads/"+flag.Arg(1), nil)
	case "reconcile":
		err = client.Call("POST", "/api/reconcile", map[string]string{})
	case "validate":
		err = deploy(client, flag.Args()[1:], "/api/validate", "POST")
	case "deploy", "apply":
		err = deploy(client, flag.Args()[1:], "/api/workloads", "POST")
	case "scale":
		if flag.NArg() < 3 {
			err = fmt.Errorf("scale requires: <name> <replicas>")
			break
		}
		replicas, _ := strconv.Atoi(flag.Arg(2))
		err = client.Call("PATCH", "/api/workloads/"+flag.Arg(1), map[string]any{"replicas": replicas})
	case "delete", "rm":
		if flag.NArg() < 2 {
			err = fmt.Errorf("delete requires workload name")
			break
		}
		err = client.Call("DELETE", "/api/workloads/"+flag.Arg(1), nil)
	case "logs":
		if flag.NArg() < 2 {
			err = fmt.Errorf("logs requires workload name")
			break
		}
		tail := "120"
		if flag.NArg() > 2 {
			tail = flag.Arg(2)
		}
		err = client.Call("GET", "/api/logs/"+flag.Arg(1)+"?tail="+tail, nil)

	// ── PTE Plan Gate ──────────────────────────────────────────────────────
	case "plans":
		err = client.Call("GET", "/api/plan", nil)
	case "approve":
		if flag.NArg() < 2 {
			err = fmt.Errorf("approve requires plan ID")
			break
		}
		err = client.Call("POST", "/api/plan/"+flag.Arg(1)+"/approve", map[string]string{})
	case "reject":
		if flag.NArg() < 2 {
			err = fmt.Errorf("reject requires plan ID [comment]")
			break
		}
		comment := ""
		if flag.NArg() > 2 {
			comment = strings.Join(flag.Args()[2:], " ")
		}
		err = client.Call("POST", "/api/plan/"+flag.Arg(1)+"/reject", map[string]string{"comment": comment})

	// ── Audit trail ───────────────────────────────────────────────────────
	case "audit":
		since := ""
		if flag.NArg() > 1 {
			since = "?since=" + flag.Arg(1)
		}
		err = client.Call("GET", "/api/audit"+since, nil)

	// ── Environment lock ──────────────────────────────────────────────────
	case "lock":
		reason := "manual CLI lock"
		if flag.NArg() > 1 {
			reason = strings.Join(flag.Args()[1:], " ")
		}
		err = client.Call("POST", "/api/lock", map[string]string{"reason": reason})
	case "unlock":
		err = client.Call("DELETE", "/api/lock", nil)
	case "lock-status":
		err = client.Call("GET", "/api/lock", nil)

	// ── Observability ─────────────────────────────────────────────────────
	case "behavior":
		err = client.Call("GET", "/api/behavior", nil)
	case "metrics":
		err = client.Call("GET", "/api/metrics", nil)
	case "runtime":
		err = client.Call("GET", "/api/runtime/status", nil)
	case "schema":
		err = client.Call("GET", "/api/schema", nil)

	// ── Token management ──────────────────────────────────────────────────
	case "gen-token":
		// gen-token <actor> <role> [agentId] [scope] [goal]
		genActor := *actor
		genRole := *role
		genAgentID := ""
		genScope := ""
		genGoal := ""
		if flag.NArg() > 1 {
			genActor = flag.Arg(1)
		}
		if flag.NArg() > 2 {
			genRole = flag.Arg(2)
		}
		if flag.NArg() > 3 {
			genAgentID = flag.Arg(3)
		}
		if flag.NArg() > 4 {
			genScope = flag.Arg(4)
		}
		if flag.NArg() > 5 {
			genGoal = strings.Join(flag.Args()[5:], " ")
		}
		token, tokenErr := core.GenerateRequestToken(genActor, genRole, genAgentID, genScope, genGoal)
		if tokenErr != nil {
			err = tokenErr
			break
		}
		fmt.Println(token)

	// ── MCP tools ─────────────────────────────────────────────────────────
	case "mcp-tools":
		err = client.CallMCP("tools/list", nil)
	case "mcp", "mcp-serve":
		err = client.StdioBridge()

	default:
		usage()
		err = fmt.Errorf("unknown command %q", command)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "doktriai:", err)
		os.Exit(1)
	}
}

func deploy(client *cli.Client, args []string, path, method string) error {
	if len(args) < 2 {
		return fmt.Errorf("deploy requires: <name> <image> [replicas] [port] [containerPort] [runtime]")
	}
	replicas := 1
	port := 0
	containerPort := 0
	runtime := "docker"
	if len(args) > 2 {
		replicas, _ = strconv.Atoi(args[2])
	}
	if len(args) > 3 {
		port, _ = strconv.Atoi(args[3])
	}
	if len(args) > 4 {
		containerPort, _ = strconv.Atoi(args[4])
	}
	if len(args) > 5 {
		runtime = args[5]
	}
	payload := map[string]any{
		"name":          args[0],
		"image":         args[1],
		"replicas":      replicas,
		"port":          port,
		"containerPort": containerPort,
		"runtime":       runtime,
	}
	return client.Call(method, path, payload)
}

func usage() {
	fmt.Println(`doktriai-cli  [--api URL] [--role ROLE] [--actor NAME] <command>

Workload Commands:
  health                            Check API health
  list | status                     List all workloads
  get <name>                        Get a single workload with live state
  deploy <name> <image> [replicas] [port] [containerPort] [runtime]
                                    Deploy a workload
  scale <name> <replicas>           Scale an existing workload
  delete | rm <name>                Delete a workload (requires PTE approval)
  logs <name> [tail]                Stream container logs
  reconcile                         Trigger manual reconciliation

PTE Approval Gate:
  plans                             List all pending/approved/rejected plans
  approve <planId>                  Approve a pending plan and apply it
  reject <planId> [comment]         Reject a pending plan

Audit & Security:
  audit [sinceSeqId]                View audit trail (optionally since a sequence ID)
  behavior                          View per-actor behavioral anomaly metrics
  lock [reason]                     Acquire environment lock
  unlock                            Release environment lock
  lock-status                       Show current lock state

Observability:
  metrics                           Prometheus-compatible metrics
  runtime                           Runtime driver status
  schema                            WorkloadSpec field schema

Token Management:
  gen-token [actor] [role] [agentId] [scope] [goal]
                                    Generate a signed HMAC auth token

MCP Tools:
  mcp-tools                         List MCP agent tools
  mcp | mcp-serve                   Run as a stdio MCP bridge server`)
}
