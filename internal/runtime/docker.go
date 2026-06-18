package runtime

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/local/kronix-control-plane/internal/core"
)

type DockerDriver struct {
	binary string
}

func NewDockerDriver(binary string) *DockerDriver {
	return &DockerDriver{binary: binary}
}

func (d *DockerDriver) Name() string {
	return "docker"
}

func (d *DockerDriver) List(ctx context.Context) ([]core.ActualWorkload, error) {
	args := []string{
		"ps", "-a",
		"--filter", "label=io.kranix.managed=true",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Label \"io.kranix.workload\"}}\t{{.Label \"io.kranix.replica\"}}",
	}
	out, err := exec.CommandContext(ctx, d.binary, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker list failed: %s", strings.TrimSpace(string(out)))
	}
	var actual []core.ActualWorkload
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 5 || parts[3] == "" {
			continue
		}
		replica, _ := strconv.Atoi(parts[4])
		actual = append(actual, core.ActualWorkload{
			ID:      parts[0],
			Name:    parts[3],
			Replica: replica,
			Runtime: "docker",
			Status:  parts[2],
		})
	}
	return actual, scanner.Err()
}

func (d *DockerDriver) Apply(ctx context.Context, spec core.WorkloadSpec, replica int) error {
	container := containerName(spec.Name, replica)
	if d.exists(ctx, container) {
		_, _ = exec.CommandContext(ctx, d.binary, "start", container).CombinedOutput()
		return nil
	}

	args := []string{
		"run", "-d",
		"--name", container,
		"--label", "io.kranix.managed=true",
		"--label", "io.kranix.workload=" + spec.Name,
		"--label", fmt.Sprintf("io.kranix.replica=%d", replica),
	}
	if spec.Port > 0 && spec.ContainerPort > 0 {
		hostPort := spec.Port + replica
		args = append(args, "-p", fmt.Sprintf("%d:%d", hostPort, spec.ContainerPort))
	}
	for key, value := range spec.Env {
		args = append(args, "-e", key+"="+value)
	}
	args = append(args, spec.Image)
	out, err := exec.CommandContext(ctx, d.binary, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %s failed: %s", spec.Name, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *DockerDriver) Delete(ctx context.Context, workload string, replica int) error {
	out, err := exec.CommandContext(ctx, d.binary, "rm", "-f", containerName(workload, replica)).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "No such container") {
		return fmt.Errorf("docker remove failed: %s", strings.TrimSpace(string(out)))
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
	return fmt.Sprintf("kranix-%s-%d", workload, replica)
}
