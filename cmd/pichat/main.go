package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"pichat/internal/server"
	"pichat/internal/store"
	"pichat/internal/update"
	"pichat/internal/version"
	"pichat/listener"
	"pichat/web"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func usage() {
	fmt.Fprint(os.Stderr, `用法：
  pichat serve [--listen 0.0.0.0:8080] [--data ./data] [--public-url http://localhost:8080]
  pichat admin-link [--data ./data] [--public-url ...]   打印一次性管理员注册链接（1 小时）
  pichat backup <dir> [--data ./data]                    VACUUM INTO + 复制附件
  pichat version                                         打印版本号
  serve 另有 --trusted-proxies <CIDR,...>（信任其 X-Forwarded-For，默认回环和私网）
             --webhook-allow <CIDR,...>（允许 webhook 访问的内网地址，默认全部禁止）
             --update on|off、--update-repo owner/name、--update-api URL、--update-mirror 前缀（在线更新，见 README「更新」）
环境变量：PICHAT_LISTEN、PICHAT_DATA、PICHAT_PUBLIC_URL、PICHAT_TRUSTED_PROXIES、PICHAT_WEBHOOK_ALLOW、
          PICHAT_UPDATE、PICHAT_UPDATE_REPO、PICHAT_UPDATE_API、PICHAT_UPDATE_MIRROR
`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd == "version" || cmd == "--version" {
		fmt.Println(version.Version)
		return
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfg := server.Config{}
	fs.StringVar(&cfg.Listen, "listen", env("PICHAT_LISTEN", "0.0.0.0:8080"), "listen address")
	fs.StringVar(&cfg.Data, "data", env("PICHAT_DATA", "./data"), "data directory")
	fs.StringVar(&cfg.PublicURL, "public-url", env("PICHAT_PUBLIC_URL", "http://localhost:8080"), "public base URL")
	var proxies, hookAllow, upd, updRepo, updAPI, updMirror string
	fs.StringVar(&upd, "update", env("PICHAT_UPDATE", "on"), "online update: on|off")
	fs.StringVar(&updRepo, "update-repo", env("PICHAT_UPDATE_REPO", update.DefaultRepo), "GitHub repository of releases")
	fs.StringVar(&updAPI, "update-api", env("PICHAT_UPDATE_API", update.DefaultAPI), "GitHub API base URL")
	fs.StringVar(&updMirror, "update-mirror", env("PICHAT_UPDATE_MIRROR", ""), "prefix prepended to release download URLs")
	fs.StringVar(&proxies, "trusted-proxies", env("PICHAT_TRUSTED_PROXIES", ""), "CIDRs whose X-Forwarded-For is trusted (default loopback+private)")
	fs.StringVar(&hookAllow, "webhook-allow", env("PICHAT_WEBHOOK_ALLOW", ""), "CIDRs webhooks may reach despite being internal")
	var pos []string
	// Allow positional args before or after flags.
	for len(args) > 0 {
		fs.Parse(args)
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	var err error
	if proxies != "" {
		cfg.TrustedProxies, err = server.ParseCIDRs(proxies)
		check(err)
	}
	cfg.WebhookAllow, err = server.ParseCIDRs(hookAllow)
	check(err)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	switch cmd {
	case "serve":
		if upd != "on" && upd != "off" {
			check(fmt.Errorf("--update / PICHAT_UPDATE 只能是 on 或 off"))
		}
		self := update.Detect(version.Version)
		// May exec an installed newer release (never returns then).
		update.Launch(cfg.Data, self, slog.Default())
		cfg.Updater = update.New(update.Config{Enabled: upd == "on", Repo: updRepo, API: updAPI, Mirror: updMirror,
			Data: cfg.Data, Self: self, Log: slog.Default()})
		db, err := store.Open(cfg.Data)
		check(err)
		defer db.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		s := server.New(cfg, db, server.Assets{Web: web.FS, Listener: listener.Script})
		check(s.Run(ctx))
	case "admin-link":
		db, err := store.Open(cfg.Data)
		check(err)
		defer db.Close()
		url, err := server.New(cfg, db, server.Assets{}).AdminLink()
		check(err)
		fmt.Println(url)
	case "backup":
		if len(pos) != 1 {
			usage()
		}
		check(store.Backup(cfg.Data, pos[0]))
		fmt.Println("备份完成：" + pos[0])
	default:
		usage()
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}
