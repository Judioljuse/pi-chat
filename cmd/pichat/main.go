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
  serve 另有 --trusted-proxies <CIDR,...>（信任其 X-Forwarded-For，默认回环和私网）
             --webhook-allow <CIDR,...>（允许 webhook 访问的内网地址，默认全部禁止）
环境变量：PICHAT_LISTEN、PICHAT_DATA、PICHAT_PUBLIC_URL、PICHAT_TRUSTED_PROXIES、PICHAT_WEBHOOK_ALLOW
`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfg := server.Config{}
	fs.StringVar(&cfg.Listen, "listen", env("PICHAT_LISTEN", "0.0.0.0:8080"), "listen address")
	fs.StringVar(&cfg.Data, "data", env("PICHAT_DATA", "./data"), "data directory")
	fs.StringVar(&cfg.PublicURL, "public-url", env("PICHAT_PUBLIC_URL", "http://localhost:8080"), "public base URL")
	var proxies, hookAllow string
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
