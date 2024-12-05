package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
)

type AbsPath string

func NewAbsPath(p string) AbsPath {
	absPath, err := filepath.Abs(p)
	if err != nil {
		log.Fatal(err)
	}
	return AbsPath(absPath)
}

type Message struct {
	Code   string `json:"code"`
	Reload bool   `json:"reload"`
}

type CliArgs struct {
	Port      string
	WatchFile AbsPath
	Help      bool
}

func ParseCliArgs() CliArgs {
	var (
		port      string
		watchFile string
		help      bool
	)

	flag.StringVar(&port, "p", "5432", "Port to listen on")
	flag.StringVar(&watchFile, "w", ".", "File to watch")
	flag.BoolVar(&help, "h", false, "Show help")

	flag.Parse()
	return CliArgs{
		Port:      fmt.Sprintf(":%s", port),
		WatchFile: NewAbsPath(watchFile),
		Help:      help,
	}
}

func InitCli() CliArgs {
	cliArgs := ParseCliArgs()

	if cliArgs.Help {
		flag.Usage()

		os.Exit(1)
	}

	return cliArgs
}

var upgrader = websocket.Upgrader{
	// 全てのオリジンからの接続を許可
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var messageChan = make(chan Message)

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// HTTP接続をWebSocketにアップグレード
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Upgrade error:", err)
		return
	}
	defer conn.Close()

	// クライアントへの送信用チャンネル
	clientMessageChan := make(chan Message)

	// サーバーのグローバルチャンネルからクライアント専用のチャンネルへメッセージを転送
	go func() {
		for msg := range messageChan {
			clientMessageChan <- msg
		}
	}()

	for {
		select {
		case msg := <-clientMessageChan:
			err := conn.WriteJSON(msg)
			if err != nil {
				fmt.Println("WriteJSON error:", err)
			}
		}
	}
}

func watchFile(watchFile AbsPath) {
	watcher, err := fsnotify.NewWatcher()
	defer watcher.Close()
	log.Println("Watching: ", watchFile)

	watchDir := path.Dir(string(watchFile))

	err = watcher.Add(watchDir)
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				log.Println("watcher.Events is not ok")
				continue
			}
			if event.Name != string(watchFile) {
				continue
			}
			log.Println("File Changed: ", event.Name)

			time.Sleep(100 * time.Millisecond)

			fileText, err := os.ReadFile(string(watchFile))
			if err != nil {
				log.Println("ReadFile error:", err)
				continue
			}

			messageChan <- Message{
				Code:   string(fileText),
				Reload: true,
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				log.Println("watcher.Errors is not ok")
				continue
			}
			log.Println("error:", err)
		}
	}
}

func main() {
	cliArgs := InitCli()

	go watchFile(cliArgs.WatchFile)

	http.HandleFunc("/ws", wsHandler)
	log.Printf("Server started at %s\n", cliArgs.Port)
	err := http.ListenAndServe(cliArgs.Port, nil)
	if err != nil {
		log.Println("ListenAndServe error:", err)
	}
}
