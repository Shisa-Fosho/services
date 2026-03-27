---
name: health-check
description: Check health status of all services and infrastructure
user_invocable: true
---

# Health Check

1. Check Docker Compose services: `docker-compose ps`
2. Check PostgreSQL: `pg_isready -h localhost -p 5432`
3. Check NATS: `curl -s http://localhost:8222/healthz`
4. Check each service metrics endpoint:
   - Trading: `curl -s http://localhost:9091/metrics | head -5`
   - Platform: `curl -s http://localhost:9092/metrics | head -5`
   - Settlement: `curl -s http://localhost:9093/metrics | head -5`
   - Indexer: `curl -s http://localhost:9094/metrics | head -5`
   - Resolution: `curl -s http://localhost:9095/metrics | head -5`
5. Check Prometheus: `curl -s http://localhost:9090/-/ready`
6. Check Grafana: `curl -s http://localhost:3000/api/health`
7. Report status summary
