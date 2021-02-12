package main

import "fmt"

type TunPair struct {
  from int
  to int
}

func (self TunPair) String() string {
  return fmt.Sprintf("%d:%d",self.from,self.to)
}

type Screenshot struct {
  format string
  data []byte
}

type BridgeDevInfo struct {
  udid string
}

// detect( onDevConnect func( bridge CliBridge ) )

type BridgeRoot interface {
  //OnConnect( dev BridgeDev )
  //OnDisconnect( dev BridgeDev )
  list() []BridgeDevInfo
}

type BridgeDev interface {
  getUdid() string
  tunnel( pairs []TunPair )
  info( names []string ) map[string]string
  gestalt( names []string ) map[string]string
  screenshot() Screenshot
  wda( name string, port int, onStart func(), onStop func(interface{}) )
  destroy()
  setProcTracker( procTracker ProcTracker )
}