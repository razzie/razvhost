# razvhost
Virtual hosting/reverse proxy with TLS termination and automatic certificate management

## Features
* Operation modes:
  * Reverse proxy
  * File and directory hosting
  * Reading public S3 buckets
  * PHP hosting (requires php-fpm)
  * Go WebAssembly hosting
  * Redirect
* HTTPS (TLS termination)
* HTTP2
* Automatic certificate management (from Let's Encrypt)
* Live config reload
* Supports all kinds of combinations of routes and target paths
* Supports [sprig](https://masterminds.github.io/sprig/) templates
* Load balancing
* Watching docker containers with VIRTUAL_HOST and VIRTUAL_PORT environment variables
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
mybucket.com -> s3://mybucket/prefix?region=eu-central-1
phpexample.com -> php:///var/www/index.php
phpexample2.com -> php:///var/www/mysite/
golang-project.com -> go-wasm:///path/to/build.wasm
```

## Build
You can either check out the git repo and build:
```Shell
git clone https://github.com/razzie/razvhost.git
cd razvhost
make
```
or use the **go** tool:
```Shell
go get github.com/razzie/razvhost
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
        Watch Docker events to find containers with VIRTUAL_HOST
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
