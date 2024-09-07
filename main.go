package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/template"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/fzerorubigd/gobgg"
	"go.uber.org/ratelimit"
)

//go:embed template/last_plays.html
var tplText string

type Single struct {
	ID    int64
	Row   int
	Col   int
	Alt   string
	URL   string
	Count int
	BGA   bool
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGKILL,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGABRT)
	defer cancel()
	var (
		username string
		outfile  string
		webOnly  bool
	)

	flag.StringVar(&username, "username", "fzerorubigd", "The BGG username")
	flag.StringVar(&outfile, "out", "out.png", "The file to save the result")
	flag.BoolVar(&webOnly, "w", false, "run the web server and not generate the file")
	flag.Parse()

	rl := ratelimit.New(10, ratelimit.Per(60*time.Second)) // creates a 10 per minutes rate limiter.
	bgg := gobgg.NewBGGClient(gobgg.SetLimiter(rl))
	plays, err := bgg.Plays(ctx, gobgg.SetUserName(username), gobgg.SetPageNumber(1))
	if err != nil {
		panic(err)
	}
	cc := make([]Single, 16)
	row, col := 1, 0
bigLoop:
	for _, play := range plays.Items {
		if play.Quantity < 1 {
			continue
		}
		for i := range cc {
			if cc[i].ID == play.Item.ID {
				cc[i].Count = cc[i].Count + 1
				continue bigLoop
			}
			if cc[i].ID == 0 {
				col++
				if col == 5 {
					col = 1
					row++
				}
				cc[i].Row = row
				cc[i].Col = col
				cc[i].ID = play.Item.ID
				cc[i].Alt = play.Item.Name
				cc[i].BGA = play.Location == "BGA"
				continue bigLoop
			}
		}
		break
	}

	idx := make([]int64, len(cc))
	for i := range cc {
		if cc[i].ID > 0 {
			idx[i] = cc[i].ID
		}
	}

	items, err := bgg.GetThings(ctx, gobgg.GetThingIDs(idx...))
	if err != nil {
		panic(err)
	}

	for i := range cc {
		for _, th := range items {
			if cc[i].ID == th.ID {
				cc[i].URL = th.Thumbnail
			}
		}
	}

	tpl := template.Must(template.New("last_plays").Parse(tplText))

	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {

		if err := tpl.Execute(w, cc); err != nil {
			log.Print(err)
		}
	})

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}

	addr := listener.Addr().(*net.TCPAddr).Port
	go func() {
		http.Serve(listener, nil)
	}()

	if webOnly {
		log.Printf("the web server is running at http://127.0.0.1:%d", addr)
		<-ctx.Done()
		listener.Close()
		return
	}

	var buf []byte
	chromCtx, cancel := chromedp.NewContext(ctx)
	defer cancel()
	err = chromedp.Run(chromCtx,
		chromedp.EmulateViewport(1024, 768),
		chromedp.Navigate(fmt.Sprintf("http://127.0.0.1:%d", addr)),
		chromedp.Screenshot("div.gallery", &buf, chromedp.NodeReady),
	)

	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(outfile, buf, 0750); err != nil {
		panic(err)
	}
}
