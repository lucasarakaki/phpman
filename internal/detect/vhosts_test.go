package detect

import (
	"testing"

	"github.com/lucasarakaki/phpman/internal/core"
)

func TestParsePoolListen(t *testing.T) {
	content := `
; listen = /commented.sock
[www]
listen = /run/php/php8.2-fpm.sock
listen = 127.0.0.1:9000
listen = "$pool"
`
	got := ParsePoolListen(content)
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 entradas", got)
	}
	if got[0] != "/run/php/php8.2-fpm.sock" {
		t.Errorf("primeiro listen = %q", got[0])
	}
	if got[1] != "127.0.0.1:9000" {
		t.Errorf("segundo listen = %q", got[1])
	}
}

func TestNormalizeTarget(t *testing.T) {
	tests := []struct {
		in     string
		target string
		kind   string
	}{
		{"unix:/run/php/php8.2-fpm.sock", "/run/php/php8.2-fpm.sock", "fpm-socket"},
		{"http://unix:/run/php/php8.3-fpm.sock", "/run/php/php8.3-fpm.sock", "fpm-socket"},
		{"/run/php/php8.2-fpm.sock|fcgi://localhost", "/run/php/php8.2-fpm.sock", "fpm-socket"},
		{"127.0.0.1:9000", "127.0.0.1:9000", "fpm-tcp"},
		{"http://localhost:8080", "", ""},
		{"unix:/run/gunicorn.sock", "", ""},
		{"unix:/run/php-fpm.sock", "/run/php-fpm.sock", "fpm-socket"},
	}
	for _, tt := range tests {
		target, kind := NormalizeTarget(tt.in)
		if target != tt.target || kind != tt.kind {
			t.Errorf("NormalizeTarget(%q) = (%q, %q), want (%q, %q)", tt.in, target, kind, tt.target, tt.kind)
		}
	}
}

func TestParseNginxVhosts(t *testing.T) {
	cfg := `server {
    listen 80;
    server_name example.test;
    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:/run/php/php8.2-fpm.sock;
    }
}

server {
    listen 81;
    server_name tcp.test;
    location ~ \.php$ {
        fastcgi_pass 127.0.0.1:9000;
    }
}
`
	vhosts := ParseNginxVhosts("/etc/nginx/sites-enabled/app.conf", cfg)
	if len(vhosts) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(vhosts), vhosts)
	}
	if vhosts[0].Server != "example.test" || vhosts[0].Kind != "fpm-socket" ||
		vhosts[0].Target != "/run/php/php8.2-fpm.sock" {
		t.Errorf("vhost[0] = %+v", vhosts[0])
	}
	if vhosts[1].Server != "tcp.test" || vhosts[1].Kind != "fpm-tcp" {
		t.Errorf("vhost[1] = %+v", vhosts[1])
	}
}

func TestParseApacheVhosts(t *testing.T) {
	cfg := `<VirtualHost *:80>
    ServerName app.test
    <FilesMatch \.php$>
        SetHandler "proxy:unix:/run/php/php8.3-fpm.sock|fcgi://localhost"
    </FilesMatch>
</VirtualHost>

<VirtualHost *:81>
    ServerName legacy.test
    SetHandler application/x-httpd-php
</VirtualHost>

ProxyPassMatch ^/(.*\.php)$ unix:/run/php/php8.2-fpm.sock|fcgi://localhost/
`
	vhosts := ParseApacheVhosts("/etc/apache2/sites-enabled/app.conf", cfg)
	if len(vhosts) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(vhosts), vhosts)
	}
	if vhosts[0].Server != "app.test" || vhosts[0].Target != "/run/php/php8.3-fpm.sock" {
		t.Errorf("vhost[0] = %+v", vhosts[0])
	}
	if vhosts[1].Kind != "mod_php" || vhosts[1].Server != "legacy.test" {
		t.Errorf("vhost[1] = %+v", vhosts[1])
	}
	if vhosts[2].Kind != "fpm-socket" {
		t.Errorf("vhost[2] = %+v", vhosts[2])
	}
}

func TestAttachSocketsSingleVersion(t *testing.T) {
	versions := []core.Version{{
		Raw:   "8.3",
		SAPIs: []core.SAPI{{Kind: core.SAPIFPM}},
	}}
	sockets := []core.Socket{{Path: "/run/php-fpm/www.sock", Exists: true}}
	AttachSockets(versions, sockets)

	fpm, ok := versions[0].SAPI(core.SAPIFPM)
	if !ok {
		t.Fatal("FPM não encontrado")
	}
	if fpm.Socket != "/run/php-fpm/www.sock" || !fpm.SocketExists {
		t.Errorf("socket não anexado: %+v", fpm)
	}
	if sockets[0].Version != "8.3" {
		t.Errorf("versão do socket = %q", sockets[0].Version)
	}
}

func TestAttachServicesVersionless(t *testing.T) {
	versions := []core.Version{{
		Raw:   "8.2",
		SAPIs: []core.SAPI{{Kind: core.SAPIFPM}},
	}}
	services := []core.Service{{Unit: "php8.2-fpm.service", Version: "", Active: true, Enabled: true, State: "active"}}
	AttachServices(versions, services, "8.2", nil)

	fpm, _ := versions[0].SAPI(core.SAPIFPM)
	if fpm.Unit != "php8.2-fpm.service" || !fpm.Active {
		t.Errorf("serviço não anexado: %+v", fpm)
	}
}
