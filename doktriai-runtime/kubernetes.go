package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"text/template"

	"github.com/praful224/doktriai/doktriai-packages"
)

// K8sDriver implements RuntimeDriver using kubectl.
// Falls back to simulation if kubectl is not available.
type K8sDriver struct {
	Namespace string
	binary    string
	simulated bool
}

func NewK8sDriver(ns string) *K8sDriver {
	d := &K8sDriver{
		Namespace: ns,
		binary:    "kubectl",
	}
	// Probe kubectl availability
	if err := exec.Command(d.binary, "version", "--client").Run(); err != nil {
		d.simulated = true
	}
	return d
}

func (k *K8sDriver) Name() string { return "kubernetes" }

// deploymentYAML is the template used to generate a Deployment manifest.
var deploymentYAML = template.Must(template.New("deployment").Parse(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: doktri-{{.Name}}
  namespace: {{.Namespace}}
  labels:
    app: {{.Name}}
    io.doktri.managed: "true"
    io.doktri.workload: {{.Name}}
spec:
  replicas: {{.Replicas}}
  selector:
    matchLabels:
      app: {{.Name}}
  template:
    metadata:
      labels:
        app: {{.Name}}
        io.doktri.managed: "true"
    spec:
      containers:
      - name: {{.Name}}
        image: {{.Image}}
        {{- if .Port}}
        ports:
        - containerPort: {{.ContainerPort}}
        {{- end}}
        {{- if .Resources.MemoryMB}}
        resources:
          limits:
            memory: "{{.Resources.MemoryMB}}Mi"
          {{- if .Resources.CPUShares}}
          requests:
            cpu: "{{.Resources.CPUShares}}m"
          {{- end}}
        {{- end}}
        {{- if .Env}}
        env:
        {{- range $k, $v := .Env}}
        - name: {{$k}}
          value: {{printf "%q" $v}}
        {{- end}}
        {{- end}}
`))

func (k *K8sDriver) manifestFor(spec packages.WorkloadSpec) ([]byte, error) {
	type tmplData struct {
		packages.WorkloadSpec
		Namespace string
	}
	var buf bytes.Buffer
	if err := deploymentYAML.Execute(&buf, tmplData{WorkloadSpec: spec, Namespace: k.Namespace}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (k *K8sDriver) List(ctx context.Context) ([]packages.ActualWorkload, error) {
	if k.simulated {
		return []packages.ActualWorkload{}, nil
	}
	out, err := exec.CommandContext(ctx, k.binary, "get", "pods",
		"-n", k.Namespace,
		"-l", "io.doktri.managed=true",
		"--no-headers",
		"-o", "custom-columns=NAME:.metadata.name,STATUS:.status.phase,IMAGE:.spec.containers[0].image",
	).CombinedOutput()
	if err != nil {
		k.simulated = true
		return []packages.ActualWorkload{}, nil
	}
	var actual []packages.ActualWorkload
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		podName := parts[0]
		status := parts[1]
		image := ""
		if len(parts) > 2 {
			image = parts[2]
		}
		// Parse workload name from pod name: doktri-<workload>-<hash>
		wl := podName
		if strings.HasPrefix(wl, "doktri-") {
			wl = strings.TrimPrefix(wl, "doktri-")
		}
		actual = append(actual, packages.ActualWorkload{
			ID:      podName,
			Name:    wl,
			Replica: 0,
			Runtime: "kubernetes",
			Status:  status,
			Image:   image,
		})
	}
	return actual, nil
}

func (k *K8sDriver) Apply(ctx context.Context, spec packages.WorkloadSpec, replica int) error {
	if k.simulated {
		fmt.Printf("[K8S-DRIVER] Simulated deploy: %s (replica %d) in ns %s\n", spec.Name, replica, k.Namespace)
		return nil
	}
	manifest, err := k.manifestFor(spec)
	if err != nil {
		return fmt.Errorf("k8s manifest generation failed: %w", err)
	}
	cmd := exec.CommandContext(ctx, k.binary, "apply", "-f", "-", "-n", k.Namespace)
	cmd.Stdin = bytes.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (k *K8sDriver) Delete(ctx context.Context, workload string, replica int) error {
	return k.DeleteWorkload(ctx, workload)
}

func (k *K8sDriver) DeleteWorkload(ctx context.Context, workload string) error {
	if k.simulated {
		fmt.Printf("[K8S-DRIVER] Simulated delete workload %s\n", workload)
		return nil
	}
	out, err := exec.CommandContext(ctx, k.binary,
		"delete", "deployment", "doktri-"+workload,
		"-n", k.Namespace, "--ignore-not-found",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl delete failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (k *K8sDriver) Logs(ctx context.Context, workload string, tail int) ([]string, error) {
	if k.simulated {
		return []string{fmt.Sprintf("[k8s-simulated] Logs for workload %s (kubectl not available)", workload)}, nil
	}
	out, err := exec.CommandContext(ctx, k.binary, "logs",
		"-l", fmt.Sprintf("io.doktri.workload=%s", workload),
		"-n", k.Namespace,
		fmt.Sprintf("--tail=%d", tail),
		"--all-containers",
	).CombinedOutput()
	if err != nil {
		return []string{string(out)}, nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			lines = append(lines, fmt.Sprintf("[k8s/%s] %s", workload, l))
		}
	}
	return lines, nil
}

func (k *K8sDriver) IsSimulated() bool { return k.simulated }
