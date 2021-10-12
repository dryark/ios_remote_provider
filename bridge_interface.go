package main

import (
  "fmt"
  uj "github.com/nanoscopic/ujsonin/v2/mod"
)

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
  GetDevs(*Config) []string
}

type iProc struct {
  pid int32
  name string
}

type BridgeDev interface {
  getUdid() string
  tunnel( pairs []TunPair, onready func() )
  info( names []string ) map[string]string
  gestalt( names []string ) map[string]string
  gestaltnode( names []string ) map[string]uj.JNode
  ps() []iProc
  screenshot() Screenshot
  cfa( onStart func(), onStop func(interface{}) )
  wda( onStart func(), onStop func(interface{}) )
  destroy()
  setProcTracker( procTracker ProcTracker )
  NewBackupVideo( port int, onStop func( interface{} ) ) BackupVideo
  GetPid( appname string ) uint64
  AppInfo( bundleId string ) uj.JNode
  InstallApp( appPath string ) bool
  NewSyslogMonitor( handleLogItem func( msg string, app string ) )
  Kill( pid uint64 )
  SetConfig( devConfig *CDevice )
  SetDevice( device *Device )
}

type BackupVideo interface {
  GetFrame() []byte
}