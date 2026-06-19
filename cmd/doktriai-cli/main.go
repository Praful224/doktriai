package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/praful224/doktriai/doktriai-cli"
)

func main() {
	apiURL := flag.String("api", "http://localhost:18080", "doktriai-api base URL")
	role := flag.String("role", "admin", "Doktri role header")
	flag.Parse()

	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}

	client := cli.NewClient(*apiURL, *role, "doktriai-cli")
	command := flag.Arg(0)
	var err error
	switch command {
	case "health":
		err = client.Call("GET", "/api/health", nil)
	case "list":
		err = client.Call("GET", "/api/workloads", nil)
	case "reconcile":
		err = client.Call("POST", "/api/reconcile", map[string]string{})
	case "deploy":
		err = deploy(client, flag.Args()[1:])
	case "delete":
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
		err = client.Call("GET", "/api/logs/"+flag.Arg(1)+"?tail=120", nil)
	default:
		usage()
		err = fmt.Errorf("unknown command %q", command)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "doktriai:", err)
		os.Exit(1)
	}
}

func deploy(client *cli.Client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("deploy requires name image [replicas] [port] [containerPort]")
	}
	replicas := 1
	port := 0
	containerPort := 0
	if len(args) > 2 {
		replicas, _ = strconv.Atoi(args[2])
	}
	if len(args) > 3 {
		port, _ = strconv.Atoi(args[3])
	}
	if len(args) > 4 {
		containerPort, _ = strconv.Atoi(args[4])
	}
	payload := map[string]any{
		"name":          args[0],
		"image":         args[1],
		"replicas":      replicas,
		"port":          port,
		"containerPort": containerPort,
		"runtime":       "docker",
	}
	return client.Call("POST", "/api/workloads", payload)
}

func usage() {
	fmt.Println(`doktriai-cli

Commands:
  health
  list
  reconcile
  deploy <name> <image> [replicas] [port] [containerPort]
  delete <name>
  logs <name>`)
}
