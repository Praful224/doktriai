package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

func main() {
	apiURL := flag.String("api", "http://localhost:18080", "kranix-api base URL")
	role := flag.String("role", "admin", "Kranix role header")
	flag.Parse()

	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}

	client := &http.Client{}
	command := flag.Arg(0)
	var err error
	switch command {
	case "health":
		err = request(client, *apiURL, *role, http.MethodGet, "/api/health", nil)
	case "list":
		err = request(client, *apiURL, *role, http.MethodGet, "/api/workloads", nil)
	case "reconcile":
		err = request(client, *apiURL, *role, http.MethodPost, "/api/reconcile", map[string]string{})
	case "deploy":
		err = deploy(client, *apiURL, *role, flag.Args()[1:])
	case "delete":
		if flag.NArg() < 2 {
			err = fmt.Errorf("delete requires workload name")
			break
		}
		err = request(client, *apiURL, *role, http.MethodDelete, "/api/workloads/"+flag.Arg(1), nil)
	case "logs":
		if flag.NArg() < 2 {
			err = fmt.Errorf("logs requires workload name")
			break
		}
		err = request(client, *apiURL, *role, http.MethodGet, "/api/logs/"+flag.Arg(1)+"?tail=120", nil)
	default:
		usage()
		err = fmt.Errorf("unknown command %q", command)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kranix:", err)
		os.Exit(1)
	}
}

func deploy(client *http.Client, apiURL, role string, args []string) error {
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
	return request(client, apiURL, role, http.MethodPost, "/api/workloads", payload)
}

func request(client *http.Client, apiURL, role, method, path string, payload any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, apiURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kranix-Role", role)
	req.Header.Set("X-Kranix-Actor", "kranix-cli")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return fmt.Errorf("%s", bytes.TrimSpace(data))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		fmt.Println(pretty.String())
		return nil
	}
	fmt.Println(string(data))
	return nil
}

func usage() {
	fmt.Println(`kranix-cli

Commands:
  health
  list
  reconcile
  deploy <name> <image> [replicas] [port] [containerPort]
  delete <name>
  logs <name>`)
}
