// cmd/install_edis.go — emily install --edis
// Full-stack EDIS provisioner: nginx + PHP-FPM + WordPress + EDIS plugins/theme
// + custodian agent registration with IDUNA.
//
// Usage:
//   emily install --edis --domain edis.example.com --signalapi https://api.fatbaby.io
//   emily install --edis --dry-run   (print every command without executing)

package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// edisInstallOpts holds parsed flags for the --edis subcommand.
type edisInstallOpts struct {
	domain      string
	webroot     string
	webServer   string // "nginx" | "apache"
	signalapiURL string
	emilyURL    string
	edisRepo    string
	dbName      string
	dbUser      string
	dryRun      bool
}

// runInstallEDIS orchestrates the full EDIS stack installation.
func runInstallEDIS(args []string) int {
	opts := edisInstallOpts{
		domain:      "localhost",
		webroot:     "/var/www/edis",
		webServer:   "nginx",
		signalapiURL: "http://localhost:9091",
		emilyURL:    "http://localhost:8086",
		edisRepo:    "/home/fatbaby/EDIS",
		dbName:      "edis",
		dbUser:      "edis",
	}

	// Parse remaining args as key=value pairs (simple, no flag library needed here)
	for _, arg := range args {
		k, v, _ := strings.Cut(arg, "=")
		switch k {
		case "--domain":
			opts.domain = v
		case "--webroot":
			opts.webroot = v
		case "--web-server":
			opts.webServer = v
		case "--signalapi":
			opts.signalapiURL = v
		case "--emily-url":
			opts.emilyURL = v
		case "--edis-repo":
			opts.edisRepo = v
		case "--dry-run":
			opts.dryRun = true
		}
	}

	// Also handle space-separated flags (--domain foo style)
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--domain":
			opts.domain = args[i+1]
		case "--webroot":
			opts.webroot = args[i+1]
		case "--web-server":
			opts.webServer = args[i+1]
		case "--signalapi":
			opts.signalapiURL = args[i+1]
		case "--emily-url":
			opts.emilyURL = args[i+1]
		case "--edis-repo":
			opts.edisRepo = args[i+1]
		}
		if args[i] == "--dry-run" {
			opts.dryRun = true
		}
	}

	if opts.dryRun {
		fmt.Println("# emily install --edis (DRY RUN — no commands executed)")
		fmt.Println()
	}

	dbPass := genSecret(16)
	wpAdminPass := genSecret(16)

	steps := []struct {
		label string
		fn    func() error
	}{
		{"1. Install system dependencies", func() error {
			return opts.step([]string{
				"apt-get", "-y", "update",
			})
		}},
		{"2. Install nginx + PHP + MySQL packages", func() error {
			pkgs := []string{
				"apt-get", "-y", "install",
				"nginx", "php8.1-fpm", "php8.1-mysql", "php8.1-curl",
				"php8.1-mbstring", "php8.1-xml", "php8.1-zip",
				"mysql-server", "curl", "unzip",
			}
			if opts.webServer == "apache" {
				pkgs = append(pkgs, "libapache2-mod-php8.1")
				// remove nginx if apache selected
				for i, p := range pkgs {
					if p == "nginx" {
						pkgs = append(pkgs[:i], pkgs[i+1:]...)
						break
					}
				}
				pkgs = append(pkgs, "apache2")
			}
			return opts.step(pkgs)
		}},
		{"3. Install WP-CLI", func() error {
			if err := opts.step([]string{
				"bash", "-c",
				"curl -sO https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar && " +
					"chmod +x wp-cli.phar && mv wp-cli.phar /usr/local/bin/wp",
			}); err != nil {
				return err
			}
			return nil
		}},
		{"4. Create MySQL database and user", func() error {
			sql := fmt.Sprintf(
				"CREATE DATABASE IF NOT EXISTS %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; "+
					"CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'; "+
					"GRANT ALL ON %s.* TO '%s'@'localhost'; FLUSH PRIVILEGES;",
				opts.dbName, opts.dbUser, dbPass, opts.dbName, opts.dbUser,
			)
			return opts.step([]string{"mysql", "-u", "root", "-e", sql})
		}},
		{"5. Download WordPress", func() error {
			if err := opts.step([]string{"mkdir", "-p", opts.webroot}); err != nil {
				return err
			}
			return opts.step([]string{
				"wp", "core", "download",
				"--path=" + opts.webroot,
				"--allow-root",
			})
		}},
		{"6. Create wp-config.php", func() error {
			extraPHP := fmt.Sprintf(
				"define('EDIS_SIGNALAPI_URL', '%s');\ndefine('EDIS_EMILY_URL', '%s');\ndefine('EDIS_CACHE_TTL', 60);",
				opts.signalapiURL, opts.emilyURL,
			)
			return opts.step([]string{
				"wp", "config", "create",
				"--path=" + opts.webroot,
				"--dbname=" + opts.dbName,
				"--dbuser=" + opts.dbUser,
				"--dbpass=" + dbPass,
				"--extra-php=" + extraPHP,
				"--allow-root",
			})
		}},
		{"7. Install WordPress core", func() error {
			return opts.step([]string{
				"wp", "core", "install",
				"--path=" + opts.webroot,
				"--url=https://" + opts.domain,
				"--title=EDIS Financial Intelligence",
				"--admin_user=admin",
				"--admin_email=emilyspringerton@gmail.com",
				"--admin_password=" + wpAdminPass,
				"--allow-root",
				"--skip-email",
			})
		}},
		{"8. Copy EDIS plugins and theme", func() error {
			pluginsDir := filepath.Join(opts.webroot, "wp-content", "plugins")
			themesDir := filepath.Join(opts.webroot, "wp-content", "themes")
			for _, plugin := range []string{"edis-core", "edis-signals", "edis-ask-emily", "edis-dis"} {
				src := filepath.Join(opts.edisRepo, "plugins", plugin)
				if err := opts.step([]string{"cp", "-r", src, pluginsDir}); err != nil {
					return err
				}
			}
			return opts.step([]string{"cp", "-r", filepath.Join(opts.edisRepo, "themes", "edis"), themesDir})
		}},
		{"9. Activate plugins and theme", func() error {
			if err := opts.step([]string{
				"wp", "plugin", "activate",
				"edis-core", "edis-signals", "edis-ask-emily", "edis-dis",
				"--path=" + opts.webroot, "--allow-root",
			}); err != nil {
				return err
			}
			if err := opts.step([]string{
				"wp", "theme", "activate", "edis",
				"--path=" + opts.webroot, "--allow-root",
			}); err != nil {
				return err
			}
			// Flush rewrite rules
			return opts.step([]string{
				"wp", "rewrite", "structure", "/%postname%/", "--hard",
				"--path=" + opts.webroot, "--allow-root",
			})
		}},
		{"10. Create Ask page", func() error {
			return opts.step([]string{
				"wp", "post", "create",
				"--post_type=page", "--post_title=Ask Emily",
				"--post_content=[ask_emily]", "--post_status=publish", "--post_name=ask",
				"--path=" + opts.webroot, "--allow-root",
			})
		}},
		{"11. Write nginx vhost", func() error {
			if opts.webServer != "nginx" {
				fmt.Println("  (skipped — web-server=apache)")
				return nil
			}
			conf := buildNginxEDISConf(opts.domain, opts.webroot)
			confPath := fmt.Sprintf("/etc/nginx/sites-available/%s.conf", opts.domain)
			enablePath := fmt.Sprintf("/etc/nginx/sites-enabled/%s.conf", opts.domain)
			if opts.dryRun {
				fmt.Printf("  # would write %s\n", confPath)
				fmt.Println(conf)
				return nil
			}
			if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
				return fmt.Errorf("write nginx conf: %w", err)
			}
			// Symlink into sites-enabled
			_ = os.Remove(enablePath)
			if err := os.Symlink(confPath, enablePath); err != nil {
				return fmt.Errorf("symlink nginx conf: %w", err)
			}
			return opts.step([]string{"nginx", "-t"})
		}},
		{"12. Install DIS systemd service", func() error {
			disBin := "/usr/local/bin/edis-dis"
			unit := fmt.Sprintf(`[Unit]
Description=EDIS Digital Immune System collector
After=network.target

[Service]
Type=simple
ExecStart=%s --log /var/log/nginx/access.log --addr 127.0.0.1:9099
Restart=on-failure
RestartSec=10
MemoryMax=64M

[Install]
WantedBy=multi-user.target
`, disBin)
			unitPath := "/etc/systemd/system/edis-dis.service"
			if opts.dryRun {
				fmt.Printf("  # would write %s\n", unitPath)
				return nil
			}
			if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
				return fmt.Errorf("write dis unit: %w", err)
			}
			return nil
		}},
		{"13. Register custodian agent with IDUNA", func() error {
			return opts.registerCustodian()
		}},
	}

	for _, s := range steps {
		fmt.Printf("→ %s\n", s.label)
		if err := s.fn(); err != nil {
			fmt.Fprintf(os.Stderr, "  ERROR: %v\n", err)
			return 4
		}
		fmt.Println("  ✓")
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("EDIS installation complete.")
	fmt.Println()
	fmt.Printf("  WordPress admin:  https://%s/wp-admin\n", opts.domain)
	fmt.Printf("  Admin user:       admin\n")
	fmt.Printf("  Admin password:   %s\n", wpAdminPass)
	fmt.Printf("  DB password:      %s   ← store in IDUNA secrets\n", dbPass)
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Printf("    1. Point DNS %s → this server IP\n", opts.domain)
	fmt.Println("    2. Install SSL: certbot --nginx -d " + opts.domain)
	fmt.Println("    3. Build EDIS DIS binary: cd /home/fatbaby/EDIS && go build -o /usr/local/bin/edis-dis ./cmd/dis")
	fmt.Println("    4. Enable DIS: systemctl enable --now edis-dis")
	fmt.Println("    5. Reload nginx: systemctl reload nginx")
	fmt.Println("    6. Run emily install --cron to schedule sync jobs")
	fmt.Println("═══════════════════════════════════════════════════════")
	return 0
}

// step runs a command, printing it first. In dry-run mode, just prints.
func (o *edisInstallOpts) step(args []string) error {
	fmt.Printf("  $ %s\n", strings.Join(args, " "))
	if o.dryRun {
		return nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// registerCustodian registers the EDIS-CUSTODIAN agent via IDUNA if it's reachable,
// and prints the generated secret for the operator to store.
func (o *edisInstallOpts) registerCustodian() error {
	cfg, _ := config.Resolve()
	if cfg == nil || cfg.IDUNABaseURL == "" {
		fmt.Println("  IDUNA not configured — skipping live registration.")
		fmt.Println("  To register manually, add EDIS-CUSTODIAN to IDUNA/config/agents.json")
		fmt.Println("  and run: cd IDUNA && go run ./cmd/bootstrap")
		return nil
	}

	secret := genSecret(32)
	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	if err := client.Auth(); err != nil {
		fmt.Printf("  IDUNA unreachable (%v) — skipping live registration.\n", err)
		fmt.Printf("  Generated custodian secret (save this): %s\n", secret)
		fmt.Println("  Add EDIS-CUSTODIAN to IDUNA/config/agents.json and run bootstrap.")
		return nil
	}

	id, err := client.PostApple(iduna.ApplePayload{
		AppleType:  "completion",
		Title:      "EDIS custodian agent registered",
		Body:       fmt.Sprintf("emily install --edis registered EDIS-CUSTODIAN on host with domain=%s. Secret stored in /etc/edis/custodian.env.", o.domain),
		SourceRepo: "emily.cli",
	})
	if err == nil {
		fmt.Printf("  Apple #%d filed.\n", id)
	}

	if o.dryRun {
		fmt.Printf("  # would write /etc/edis/custodian.env\n")
		return nil
	}

	if err := os.MkdirAll("/etc/edis", 0o700); err != nil {
		return fmt.Errorf("mkdir /etc/edis: %w", err)
	}
	envContent := fmt.Sprintf(
		"EDIS_CUSTODIAN_AGENT_NAME=EDIS-CUSTODIAN\nEDIS_CUSTODIAN_AGENT_SECRET=%s\nIDUNA_BASE_URL=%s\n",
		secret, cfg.IDUNABaseURL,
	)
	if err := os.WriteFile("/etc/edis/custodian.env", []byte(envContent), 0o600); err != nil {
		return fmt.Errorf("write custodian env: %w", err)
	}
	fmt.Println("  Custodian env written to /etc/edis/custodian.env (chmod 600)")
	fmt.Printf("  Agent secret: %s\n", secret)
	fmt.Println("  ⚠ Add the secret to IDUNA via IDUNA/config/agents.json + bootstrap.")
	return nil
}

// buildNginxEDISConf returns a minimal nginx server block for WordPress + PHP-FPM.
func buildNginxEDISConf(domain, webroot string) string {
	return fmt.Sprintf(`# EDIS WordPress nginx config — generated by emily install --edis
# Place in /etc/nginx/sites-available/%s.conf

server {
    listen 80;
    server_name %s;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name %s;
    root %s;
    index index.php;

    # SSL — fill in after certbot
    # ssl_certificate     /etc/letsencrypt/live/%s/fullchain.pem;
    # ssl_certificate_key /etc/letsencrypt/live/%s/privkey.pem;

    # WordPress rewrite
    location / {
        try_files $uri $uri/ /index.php?$args;
    }

    # PHP-FPM
    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:/run/php/php8.1-fpm.sock;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }

    # Block direct PHP access to uploads
    location ~* /(?:uploads|files)/.*\.php$ { deny all; }

    # DIS collector health endpoint (internal only)
    location /dis/ {
        proxy_pass http://127.0.0.1:9099;
        allow 127.0.0.1;
        deny all;
    }

    # Rate limiting for REST API
    limit_req_zone $binary_remote_addr zone=edis_api:10m rate=10r/s;
    location /wp-json/ {
        limit_req zone=edis_api burst=20 nodelay;
        try_files $uri $uri/ /index.php?$args;
    }

    # Security headers
    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options SAMEORIGIN;
    add_header Referrer-Policy strict-origin-when-cross-origin;
}
`, domain, domain, domain, webroot, domain, domain)
}

// genSecret generates n random hex bytes.
func genSecret(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
