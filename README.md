# razvhost
Virtual hosting/reverse proxy with TLS termination and automatic certificate management

## Features
* Operation modes:
  * Reverse proxy
  * Redirect
  * File and directory hosting
  * Reading S3 buckets
  * Reading SFTP directories
  * PHP hosting (requires php-fpm)
  * Go WebAssembly hosting
  * Tailing log files
* HTTPS (TLS termination)
* HTTP2
* Automatic certificate management (from Let's Encrypt)
* Live config reload
* Supports all kinds of combinations of routes and target paths
* Supports [sprig](https://masterminds.github.io/sprig/) templates
* Load balancing
* Watching Docker containers with VIRTUAL_HOST and VIRTUAL_PORT labels
* Configurable header discarding
* Request logging

## Configuration
By default razvhost tries to read configuration from `config` file in the working directory.
Alternatively you can specify the config file location with `-cfg <config file>` command line arg.

An example configuration:
```
# comment
example.com alias.com {{env "ALIAS"}} -> http://localhost:8080
example.com/*/files -> file:///var/www/public/
loadbalance.com -> http://localhost:8081 http://localhost:8082
*.redirect.com -> redirect://github.com/razzie/razvhost
public-bucket.com -> s3://public-bucket/prefix?region=eu-central-1
private-bucket.com -> s3://key:secret@private-bucket/prefix?region=eu-central-1
mysftp.com -> sftp://user:pass@sftphost.com/dir
phpexample.com -> php:///var/www/index.php
phpexample2.com -> php:///var/www/mysite/
golang-project.com -> go-wasm:///path/to/build.wasm
kernel-logs-new.net -> tail-new:///var/log/kern.log
kernel-logs-all.net -> tail:///var/log/kern.log
```

### Docker discovery

Run razvhost with `-docker` to discover running containers and watch for container
start and stop events. Set the `VIRTUAL_HOST` label on each container to a
space-separated list of hostnames. The optional `VIRTUAL_PORT` label selects the
container port and defaults to `8080`. That port must be published on the Docker
host; razvhost connects to its published port on `localhost`.

For example, in Docker Compose:

```yaml
services:
  web:
    image: nginx:alpine
    labels:
      VIRTUAL_HOST: "example.com alias.com"
      VIRTUAL_PORT: "80"
    ports:
      - "127.0.0.1:8080:80"
```

The equivalent Docker command is:

```sh
docker run -d --label "VIRTUAL_HOST=example.com alias.com" \
  --label VIRTUAL_PORT=80 -p 127.0.0.1:8080:80 nginx:alpine
```

For existing deployments, move `VIRTUAL_HOST` and `VIRTUAL_PORT` from environment
variables to labels and recreate the containers. Container environment variables
are no longer used for Docker discovery.

## Build
You can either check out the git repo and build:
```Shell
git clone https://github.com/razzie/razvhost.git
cd razvhost
make
```
or use the **go** tool:
```Shell
go install github.com/razzie/razvhost/cmd/razvhost@latest
```

## Run
You don't need to run razvhost as root user, but you will need to set special capabilities on the binary to be able to bind 80 and 443 ports.
Use either `sudo setcap 'cap_net_bind_service=+ep' ./razvhost` or the existing setcap.sh helper in this repo: `sudo ./setcap.sh`

Command line args:
```
./razvhost -h
Usage of ./razvhost:
  -certs string
        Directory to store certificates in (default "certs")
  -cfg string
        Config file (default "config")
  -debug string
        Debug listener address, where hostname is the first part of the URL
  -discard-headers string
        Comma separated list of http headers to discard
  -docker
        Watch Docker events to find containers with the VIRTUAL_HOST label
  -http2
        Enable HTTP2
  -no-server-header
        Disable 'Server: razvhost/<version>' header in responses
  -nocert
        Disable HTTPS and certificate handling
  -php-addr string
        PHP CGI address (default "unix:///var/run/php/php-fpm.sock")
```

If you intend to run razvhost using **supervisor**, here is an example configuration:
```INI
[program:razvhost]
command=/razvhost/razvhost -http2
user=
stderr_logfile=/var/log/supervisor/razvhost-err.log
stdout_logfile=/var/log/supervisor/razvhost-stdout.log
directory=/razvhost/
autostart=true
autorestart=true
```
