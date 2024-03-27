package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var remoteWS string
var username string
var password string
var headless bool

func init() {
	flag.StringVar(&remoteWS, "remote-ws", "", "set remote websocket url, if not set, it will use local chrome. (if use chromedp/headless-shell docker image, set it to ws://0.0.0.0:9222)")
	flag.StringVar(&username, "username", "", "set username for login")
	flag.StringVar(&password, "password", "", "set password for login")
	flag.BoolVar(&headless, "headless", false, "set headless mode")

	flag.Parse()

	if username == "" || password == "" {
		panic("username or password is empty")
	}
}

func screenshot(ctx context.Context) {
	for range time.Tick(time.Second * 10) {
		targets, err := chromedp.Targets(ctx)
		if err != nil {
			log.Printf("screenshot: Targets error: %v\n", err)
		}
		for _, t := range targets {
			if strings.HasSuffix(t.URL, ".js") {
				continue
			}
			tctx, _ := chromedp.NewContext(ctx, chromedp.WithTargetID(t.TargetID))
			var sc []byte
			if err := chromedp.Run(tctx,
				chromedp.FullScreenshot(&sc, 100),
			); err != nil {
				log.Printf("screenshot: screenshot on target <%v>_<%v> error %v\n", t.TargetID, t.URL, err)
				continue
			}
			os.WriteFile(fmt.Sprintf("preview_%v.jpg", t.TargetID), sc, 0644)
		}
	}
}

func monitor(ctx context.Context) {
	for range time.Tick(time.Second * 10) {
		var text []string
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`[...document.querySelectorAll('p')].map((e) => e.textContent)`, &text),
		); err != nil {
			log.Printf("monitor: can not get text content: %v\n", err)
			continue
		}
		matchStatus := func(text []string) string {
			for _, s := range text {
				if strings.Contains(s, "Connecting") {
					return "Connecting"
				}
				if strings.Contains(s, "Connected") {
					return "Connected"
				}
				if strings.Contains(s, "Disconnected") {
					return "Disconnected"
				}
			}
			return "UNKNOWN"
		}
		matchNetwork := func(text []string) string {
			for _, s := range text {
				if strings.Contains(s, "Network quality") {
					return s
				}
			}
			return "UNKNOWN"
		}
		status := matchStatus(text)
		switch status {
		case "Connecting":
			log.Println("status: Connecting")
		case "Connected":
			log.Println("status: Connected")
			log.Printf("network: %v\n", matchNetwork(text))
		case "Disconnected":
			log.Println("status: Disconnected")
		case "UNKNOWN":
			log.Println("status: unknown, please check the browser")
		}
	}
}

func main() {
	var ctx = context.Background()

	if remoteWS != "" {
		ctx, _ = chromedp.NewRemoteAllocator(context.Background(), remoteWS)
	}

	dir := filepath.Dir(os.Args[0])
	userDataDir := filepath.Join(dir, "chrome-user-data-grass")

	var opts = chromedp.DefaultExecAllocatorOptions[:]
	opts = append(opts,
		chromedp.DisableGPU,
		chromedp.Flag("disable-plugins", false),
		chromedp.Flag("disable-extensions", false),
		chromedp.Flag("restore-on-startup", false),
		chromedp.UserDataDir(userDataDir),
	)

	if headless {
		fmt.Println("running in headless mode")
		opts = append(opts, chromedp.Flag("headless", "new"))
	} else {
		opts = append(opts, chromedp.Flag("headless", false))
	}

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	taskCtx, cancel := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(log.Printf),
	)
	defer cancel()

	go screenshot(taskCtx)

	targetCh := chromedp.WaitNewTarget(taskCtx, func(info *target.Info) bool {
		return strings.Contains(info.URL, "getgrass")
	})

	//var outerBefore, outerAfter string
	if err := chromedp.Run(taskCtx,
		chromedp.Navigate("chrome-extension://ilehaonighjijnmpnagapkhpcdbhclfg/index.html"),
		chromedp.WaitReady("body"),
		chromedp.Sleep(time.Second),
	); err != nil {
		log.Fatal(err)
	}

	var btnText []string
	if err := chromedp.Run(taskCtx,
		chromedp.Evaluate(`[...document.querySelectorAll('button')].map((e) => e.textContent)`, &btnText),
	); err != nil {
		log.Fatal(err)
	}
	var hasLogin bool
	for _, s := range btnText {
		if strings.Contains(strings.ToLower(s), "login") {
			hasLogin = true
		}
	}
	if hasLogin {
		if err := chromedp.Run(taskCtx,
			chromedp.Click("button"),
		); err != nil {
			log.Fatal(err)
		}
		getGrassId := <-targetCh
		grassCtx, cancel := chromedp.NewContext(taskCtx, chromedp.WithTargetID(getGrassId))
		defer cancel()

		if err := chromedp.Run(grassCtx,
			chromedp.WaitReady("body"),
			chromedp.SendKeys("input[name='user']", username),
			chromedp.SendKeys("input[name='password']", password),
			chromedp.Sleep(time.Second*3),
			chromedp.Click("button[type='submit']"),
			chromedp.Sleep(time.Second*3),
			target.CloseTarget(getGrassId),
		); err != nil {
			log.Fatal(err)
		}
	}

	var username string
	if err := chromedp.Run(taskCtx, chromedp.TextContent("p", &username)); err != nil {
		log.Fatal("can not get username")
	}

	log.Printf("login as <%v>\n", username)

	go monitor(taskCtx)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Println("bye")
}
