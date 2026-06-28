# Build CLI binary
Write-Host "Building CLI binary..." -ForegroundColor Cyan
go build -o doktriai-cli.exe cmd/doktriai-cli/main.go

# Start the demo
Write-Host "`nStep 1: Clearing DOKTRIAI_TOKEN to use local dev-mode fallback..." -ForegroundColor Cyan
$env:DOKTRIAI_TOKEN = $null

Write-Host "`nStep 2: Deploying workload 'my-web-app' with canary strategy..." -ForegroundColor Cyan
# Create a manifest
@'
apiVersion: doktriai/v1
kind: Workload
metadata:
  name: my-web-app
spec:
  image: nginx:1.20
  replicas: 5
  port: 8080
  containerPort: 80
  runtime: docker
  deployStrategy: canary
'@ | Out-File -FilePath "my-web-app.yaml" -Encoding utf8

./doktriai-cli.exe deploy -f my-web-app.yaml

Write-Host "`nStep 3: Checking initial status..." -ForegroundColor Cyan
./doktriai-cli.exe status my-web-app

Write-Host "`nStep 4: Triggering canary rollout by updating image to nginx:1.21..." -ForegroundColor Cyan
@'
apiVersion: doktriai/v1
kind: Workload
metadata:
  name: my-web-app
spec:
  image: nginx:1.21
  replicas: 5
  port: 8080
  containerPort: 80
  runtime: docker
  deployStrategy: canary
'@ | Out-File -FilePath "my-web-app.yaml" -Encoding utf8

./doktriai-cli.exe deploy -f my-web-app.yaml

Write-Host "`nStep 5: Verifying canary initialized at 10% weight..." -ForegroundColor Cyan
./doktriai-cli.exe canary-status my-web-app

Write-Host "`nStep 6: Promoting canary to 50%..." -ForegroundColor Cyan
Start-Sleep -Seconds 2
./doktriai-cli.exe canary-promote my-web-app
./doktriai-cli.exe canary-status my-web-app

Write-Host "`nStep 7: Promoting canary to 100%..." -ForegroundColor Cyan
Start-Sleep -Seconds 2
./doktriai-cli.exe canary-promote my-web-app
./doktriai-cli.exe canary-status my-web-app

# Clean up
Remove-Item -Path "my-web-app.yaml" -ErrorAction SilentlyContinue
Write-Host "`nDemo Completed successfully!" -ForegroundColor Green
