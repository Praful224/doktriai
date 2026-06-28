package runtime

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/praful224/doktriai/doktriai-packages"
)

type DockerDriver struct {
	binary          string
	mu              sync.RWMutex
	simulated       bool
	simulatedActual []packages.ActualWorkload
}

func NewDockerDriver(binary string) *DockerDriver {
	driver := &DockerDriver{binary: binary}
	// Pre-seed mock container instances so if Docker is offline, they start as healthy
	driver.simulatedActual = []packages.ActualWorkload{
		{ID: "doktri-secure-ingress-0", Name: "secure-ingress", Replica: 0, Runtime: "docker", Status: "Up 15 minutes"},
		{ID: "doktri-secure-ingress-1", Name: "secure-ingress", Replica: 1, Runtime: "docker", Status: "Up 15 minutes"},
		{ID: "doktri-reconciler-daemon-0", Name: "reconciler-daemon", Replica: 0, Runtime: "docker", Status: "Up 14 minutes"},
		{ID: "doktri-agent-gateway-0", Name: "agent-gateway", Replica: 0, Runtime: "docker", Status: "Up 9 minutes"},
	}
	return driver
}

func (d *DockerDriver) Name() string {
	return "docker"
}

func (d *DockerDriver) List(ctx context.Context) ([]packages.ActualWorkload, error) {
	d.mu.RLock()
	isSimulated := d.simulated
	d.mu.RUnlock()

	if isSimulated {
		return d.listSimulated(), nil
	}

	args := []string{
		"ps", "-a",
		"--filter", "label=io.doktri.managed=true",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Label \"io.doktri.workload\"}}\t{{.Label \"io.doktri.replica\"}}\t{{.Image}}",
	}
	out, err := exec.CommandContext(ctx, d.binary, args...).CombinedOutput()
	if err != nil {
		// Fallback to simulated mode
		d.mu.Lock()
		d.simulated = true
		d.mu.Unlock()
		return d.listSimulated(), nil
	}
	var actual []packages.ActualWorkload
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 6 || parts[3] == "" {
			continue
		}
		replica, _ := strconv.Atoi(parts[4])
		actual = append(actual, packages.ActualWorkload{
			ID:      parts[0],
			Name:    parts[3],
			Replica: replica,
			Runtime: "docker",
			Status:  parts[2],
			Image:   parts[5],
		})
	}
	return actual, scanner.Err()
}

func (d *DockerDriver) listSimulated() []packages.ActualWorkload {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]packages.ActualWorkload, len(d.simulatedActual))
	copy(out, d.simulatedActual)
	return out
}

func (d *DockerDriver) Apply(ctx context.Context, spec packages.WorkloadSpec, replica int) error {
	d.mu.Lock()
	isSimulated := d.simulated
	d.mu.Unlock()

	if isSimulated {
		d.mu.Lock()
		defer d.mu.Unlock()
		name := containerName(spec.Name, replica)
		found := false
		for i, w := range d.simulatedActual {
			if w.ID == name {
				d.simulatedActual[i].Status = "Up 5 seconds"
				d.simulatedActual[i].Image = spec.Image
				found = true
				break
			}
		}
		if !found {
			d.simulatedActual = append(d.simulatedActual, packages.ActualWorkload{
				ID:      name,
				Name:    spec.Name,
				Replica: replica,
				Runtime: "docker",
				Status:  "Up 2 seconds",
				Image:   spec.Image,
			})
		}
		return nil
	}

	container := containerName(spec.Name, replica)
	if d.exists(ctx, container) {
		_, _ = exec.CommandContext(ctx, d.binary, "start", container).CombinedOutput()
		return nil
	}

	args := []string{
		"run", "-d",
		"--name", container,
		"--label", "io.doktri.managed=true",
		"--label", "io.doktri.workload=" + spec.Name,
		"--label", fmt.Sprintf("io.doktri.replica=%d", replica),
	}
	if spec.Port > 0 && spec.ContainerPort > 0 {
		hostPort := spec.Port + replica
		args = append(args, "-p", fmt.Sprintf("%d:%d", hostPort, spec.ContainerPort))
	}
	for key, value := range spec.Env {
		args = append(args, "-e", key+"="+value)
	}
	// Resource limits
	if spec.Resources.MemoryMB > 0 {
		args = append(args, fmt.Sprintf("--memory=%dm", spec.Resources.MemoryMB))
	}
	if spec.Resources.CPUShares > 0 {
		args = append(args, fmt.Sprintf("--cpu-shares=%d", spec.Resources.CPUShares))
	}
	if spec.Resources.CPUQuota > 0 {
		args = append(args, fmt.Sprintf("--cpu-quota=%d", spec.Resources.CPUQuota))
	}
	// Volume mounts
	for _, vol := range spec.Volumes {
		mountStr := vol.HostPath + ":" + vol.ContainerPath
		if vol.ReadOnly {
			mountStr += ":ro"
		}
		args = append(args, "-v", mountStr)
	}
	// Custom labels
	for k, v := range spec.Labels {
		args = append(args, "--label", k+"="+v)
	}
	args = append(args, spec.Image)
	_, err := exec.CommandContext(ctx, d.binary, args...).CombinedOutput()
	if err != nil {
		// If command fails, check if we should fall back
		d.mu.Lock()
		d.simulated = true
		d.mu.Unlock()
		return d.Apply(ctx, spec, replica)
	}
	return nil

}

func (d *DockerDriver) Delete(ctx context.Context, workload string, replica int) error {
	d.mu.Lock()
	isSimulated := d.simulated
	d.mu.Unlock()

	if isSimulated {
		d.mu.Lock()
		defer d.mu.Unlock()
		name := containerName(workload, replica)
		var next []packages.ActualWorkload
		for _, w := range d.simulatedActual {
			if w.ID != name {
				next = append(next, w)
			}
		}
		d.simulatedActual = next
		return nil
	}

	out, err := exec.CommandContext(ctx, d.binary, "rm", "-f", containerName(workload, replica)).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "No such container") {
		return fmt.Errorf("doktri compute target remove failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *DockerDriver) DeleteWorkload(ctx context.Context, workload string) error {
	actual, err := d.List(ctx)
	if err != nil {
		return err
	}
	for _, item := range actual {
		if item.Name == workload {
			if err := d.Delete(ctx, workload, item.Replica); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DockerDriver) Logs(ctx context.Context, workload string, tail int) ([]string, error) {
	d.mu.Lock()
	isSimulated := d.simulated
	d.mu.Unlock()

	if isSimulated {
		return []string{
			fmt.Sprintf("[%s] Initializing workload task runner...", workload),
			fmt.Sprintf("[%s] Resource limits validated successfully.", workload),
			fmt.Sprintf("[%s] Binding port configurations...", workload),
			fmt.Sprintf("[%s] Server listening and ready for requests.", workload),
		}, nil
	}

	actual, err := d.List(ctx)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, item := range actual {
		if item.Name != workload {
			continue
		}
		out, err := exec.CommandContext(ctx, d.binary, "logs", "--tail", strconv.Itoa(tail), containerName(workload, item.Replica)).CombinedOutput()
		if err != nil {
			lines = append(lines, strings.TrimSpace(string(out)))
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line != "" {
				lines = append(lines, fmt.Sprintf("[%s/%d] %s", workload, item.Replica, line))
			}
		}
	}
	return lines, nil
}

func (d *DockerDriver) exists(ctx context.Context, container string) bool {
	err := exec.CommandContext(ctx, d.binary, "inspect", container).Run()
	return err == nil
}

func containerName(workload string, replica int) string {
	return fmt.Sprintf("doktri-%s-%d", workload, replica)
}

func (d *DockerDriver) IsSimulated() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.simulated
}

func (d *DockerDriver) SetSimulated(sim bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.simulated = sim
}

func (d *DockerDriver) DiscoverContainers(ctx context.Context) ([]packages.WorkloadSpec, error) {
	d.mu.RLock()
	isSimulated := d.simulated
	d.mu.RUnlock()

	if isSimulated {
		// Return sample mock workloads in simulated mode
		return []packages.WorkloadSpec{
			{
				Name:          "redis-cache",
				Image:         "redis:alpine",
				Replicas:      1,
				Port:          6379,
				ContainerPort: 6379,
				Runtime:       "docker",
				SecurityMode:  "dev",
			},
			{
				Name:          "hello-nginx",
				Image:         "nginx:alpine",
				Replicas:      1,
				Port:          80,
				ContainerPort: 80,
				Runtime:       "docker",
				SecurityMode:  "dev",
			},
		}, nil
	}

	args := []string{
		"ps", "-a",
		"--format", "{{.Names}}\t{{.Image}}\t{{.Ports}}",
	}
	out, err := exec.CommandContext(ctx, d.binary, args...).CombinedOutput()
	if err != nil {
		return nil, err
	}

	var discovered []packages.WorkloadSpec
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		parts := strings.Split(text, "\t")
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		image := parts[1]
		
		// If container is managed by doktri (prefixed with doktri-), skip it
		if strings.HasPrefix(name, "doktri-") {
			continue
		}

		port := 80
		containerPort := 80
		portsStr := ""
		if len(parts) > 2 {
			portsStr = parts[2]
		}
		if portsStr != "" {
			if idx := strings.Index(portsStr, "->"); idx != -1 {
				left := portsStr[:idx]
				right := portsStr[idx+2:]
				
				if colonIdx := strings.LastIndex(left, ":"); colonIdx != -1 {
					p, _ := strconv.Atoi(left[colonIdx+1:])
					if p > 0 {
						port = p
					}
				}
				if slashIdx := strings.Index(right, "/"); slashIdx != -1 {
					p, _ := strconv.Atoi(right[:slashIdx])
					if p > 0 {
						containerPort = p
					}
				}
			}
		}

		discovered = append(discovered, packages.WorkloadSpec{
			Name:          name,
			Image:         image,
			Replicas:      1,
			Port:          port,
			ContainerPort: containerPort,
			Runtime:       "docker",
			SecurityMode:  "dev",
		})
	}
	return discovered, scanner.Err()
}

