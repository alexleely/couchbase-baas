Write-Host "Starting Couchbase Developer Platform..." -ForegroundColor Green
docker-compose up -d --build
Write-Host "Services started successfully!" -ForegroundColor Cyan
Write-Host "Couchbase Admin UI: http://localhost:8091" -ForegroundColor Yellow
Write-Host "MinIO S3 Console:   http://localhost:9001" -ForegroundColor Yellow
Write-Host "API Gateway Health: http://localhost:8000/health" -ForegroundColor Yellow
