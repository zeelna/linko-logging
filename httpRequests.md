## curl commands for http://localhost:8899

### 0) Start the HTTP server
```bash
LINKO_LOG_FILE=linko.access.log go run .
```

### 1) GET /
```bash
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:8899/
```

### 2) GET api/stats
```bash
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:8899/api/stats
```

### 3) POST admin/shutdown
```bash
curl -sS -X POST -o /dev/null -w "%{http_code}\n" http://localhost:8899/admin/shutdown
```

### 4) Expected outcome:
```bash
user@PC:~/GolandProjects/linko-starter$ LINKO_LOG_FILE=linko.access.log go run .
2026/07/20 11:58:37 Linko is running on http://localhost:8899
2026/07/20 11:58:43 Served request: GET /
2026/07/20 11:58:45 Served request: GET /api/stats
2026/07/20 11:58:51 Served request: POST /admin/shutdown
2026/07/20 11:58:51 Linko is shutting down
```

### 5) POST /api/login with basic authentication = saruman:invalidPassword (base64 encoded)
```bash
curl -i -X POST \
-u 'saruman:invalidFormat' \
http://localhost:8899/api/login
```

```bash
curl -i -X POST \
-u 'saruman:invalidPassword' \
http://localhost:8899/api/login
```

### 6) POST /api/login as frodo:
```bash
curl -i -X POST http://localhost:8899/api/login \
-H 'Content-Type: application/json' \
-u 'frodo:ofTheNineFingers'
```

### 7) POST /api/shorten 
```bash
curl -i -X POST "http://localhost:8899/api/shorten" \
-u 'frodo:ofTheNineFingers' \
-d "url=https://www.boot.dev/blog/golang"
```

### 8) POST /api/login successful
```bash
curl -i -X POST http://localhost:8899/api/login -H 'Content-Type: application/json' -u 'frodo:ofTheNineFingers'
```

### 9) POST /api/login invalid
```bash
curl -i -X POST \
-u 'saruman:invalidFormat' \
http://localhost:8899/api/login
```

### 10) POST /api/login unsuccessful (wrong credentials)
```bash
curl -i -X POST \
-u 'frodo:wrongPassword' \
http://localhost:8899/api/login
```


### 11) POST /api/shorten -> To test URL-embedded password redaction. "url": "https://bugsBunny:wha1sUpD0c@www.boot.dev/blog/golang"
```bash
curl -i -X POST "http://localhost:8899/api/shorten" \
-u 'frodo:ofTheNineFingers' \
-d "url=https://bugsBunny:wha1sUpD0c@www.boot.dev/blog/golang"
```


### 10) GET / with 'X-Request-ID: bootdev-test-id' with both Request Headers (> lines) and Response Headers (< lines)
```bash
curl -sS -D -v -o /dev/null \
  -H 'X-Request-ID: bootdev-test-id' \
  http://localhost:8899/
```

### 10) GET / with 'X-Request-ID: bootdev-test-id' 
```bash
curl -sS -v -D - -o /dev/null \
  -H 'X-Request-ID: bootdev-test-id' \
  http://localhost:8899/
```

### 11) POST /api/shorten with url: not-a-valid-url:
```bash
curl -u frodo:ofTheNineFingers \
-d "url=not-a-valid-url" \
http://localhost:8899/api/shorten
```

### 12) POST /api/login with 127.0.0.1:8899 to verify redactIP() works.
```bash
curl -i -X POST http://127.0.0.1:8899/api/login \
-u frodo:ofTheNineFingers
```