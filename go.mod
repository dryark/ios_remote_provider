module main.go

go 1.12

//replace github.com/go-cmd/cmd => ../cmd
//replace github.com/nanoscopic/ujsonin/v2 => ../ujsonin/v2

require (
	github.com/elastic/go-sysinfo v1.5.0
	github.com/go-cmd/cmd v1.3.0
	github.com/gorilla/websocket v1.4.2
	github.com/nanoscopic/uclop v1.1.0
	github.com/nanoscopic/ujsonin/v2 v2.0.4
	github.com/sirupsen/logrus v1.7.0
	go.nanomsg.org/mangos/v3 v3.1.3
)
