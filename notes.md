# Important note about 'prometheus.yml'
## Change inside 'prometheus.yml' the vlaue for 'global.scrape_interval: 1s'
```text
We use 1s here to make scraping easy to observe during development and testing.
That's usually too aggressive for production
A more typical interval is 15s-60s depending on scale and cost constraints.
```

```text
global.scrape_interval: 60s
```

# Solve error occuring with running command 'docker compose up'
## ERR: unable to get image 'prom/prometheus:latest': permission denied while trying to connect to the docker API at unix:///var/run/docker.sock
##Solution:
Either 1) prefix with 'sudo' -> 'sudo docker compose up, or 2) Add non-root user to the docker group.
##1.1 Add non-root user to docker group
```bash
sudo usermod -aG docker user
```bash
newgrp docker
```

##1.2 Verify use has (docker) group added
```bash
id
``` 

--
# Check if Prometheus is running, by viewing web interface at:
-- be sure to find address from file 'prometheus.yml' line: scrape_configs.static_configs.targers: ["localhost:9090"]
-- In our case, open browser and open: http://localhost:9090/

#################################################
### Prometheus UI, using it #####
#################################################
IMPORTANT: Aside from debugging, it's not normal to query Prometheus directly. Another tool like Grafana will typically run these queries for us and visualize the results.
#1 Run the docker container
```bash
docker compose up
```
#2 Wait at least 10 seconds for Prometheus to scrape and store some data, then run these queries in the Prometheus UI.

##2.1 Available Memory (in bytes). Enter the following query in Prometheus UI.
```text
node_memory_MemAvailable_bytes
```

##2.2 Available Disk Space (in bytes). Enter the following query in Prometheus UI.
```text
node_filesystem_avail_bytes
```

##2.3 Prometheus also lets you run functions over stored data like sums and averages. For example, average CPU Usage for the last 5 minutes (percentage):
```text
1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m]))
```

################################################