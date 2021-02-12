// +build macos

package main

import (
    "fmt"
    uj "github.com/nanoscopic/ujsonin/mod"
    log "github.com/sirupsen/logrus"
    "os"
    "os/exec"
    "strings"
    "time"
)

type IIFBridge struct {
  onConnect func( dev BridgeDev ) ProcTracker
  onDisconnect func( dev BridgeDev )
  cli string
  devs map[string]*IIFDev
  procTracker ProcTracker
}

type IIFDev struct {
  bridge *IIFBridge
  udid string
  name string
  procTracker ProcTracker
}

// IosIF bridge
func NewIIFBridge( OnConnect func( dev BridgeDev ) (ProcTracker), OnDisconnect func( dev BridgeDev ), iosIfPath string, procTracker ProcTracker ) ( *IIFBridge ) {
  self := &IIFBridge{
    onConnect: OnConnect,
    onDisconnect: OnDisconnect,
    cli: iosIfPath,
    devs: make( map[string]*IIFDev ),
    procTracker: procTracker,
  }
  self.startDetect();
  return self
}

func (self *IIFDev) getUdid() string {
  return self.udid
}

func (self *IIFBridge) startDetect() {
  o := ProcOptions{
    procName: "device_trigger",
    binary: self.cli,
    args: []string{ "detectloop" },
    stdoutHandler: func( line string, plog *log.Entry ) {
    },
    stderrHandler: func( line string, plog *log.Entry ) {
      if strings.HasPrefix( line, "{" ) {
        root, _ := uj.Parse( []byte(line) )
        evType := root.Get("type").String()
        udid := root.Get("udid").String()
        if evType == "connect" {
          name := root.Get("name").String()
          self.OnConnect( udid, name, plog )
        } else if evType == "disconnect" {
          self.OnDisconnect( udid, plog )
        }
      }
    },
    onStop: func( interface{} ) {
      log.Println("devive trigger stopped")
    },
  }
  proc_generic( self.procTracker, nil, &o )
}

func (self *IIFBridge) list() []BridgeDevInfo {
  infos := []BridgeDevInfo{}
  for _,dev := range self.devs {
    infos = append( infos, BridgeDevInfo{ udid: dev.udid } )
  }
  return infos
}

func (self *IIFBridge) OnConnect( udid string, name string, plog *log.Entry ) {
  dev := NewIIFDev( self, udid, name )
  self.devs[ udid ] = dev
  dev.procTracker = self.onConnect( dev )
}

func (self *IIFBridge) OnDisconnect( udid string, plog *log.Entry ) {
  dev := self.devs[ udid ]
  dev.destroy()
  self.onDisconnect( dev )
  delete( self.devs, udid )
}

func (self *IIFBridge) destroy() {
  for _,dev := range self.devs {
    dev.destroy()
  }
  // close self processes
}

func NewIIFDev( bridge *IIFBridge, udid string, name string ) (*IIFDev) {
  fmt.Printf("Creating IIFDev with udid=%s\n", udid )
  var procTracker ProcTracker = nil
  return &IIFDev{
    bridge: bridge,
    name: name,
    udid: udid,
    procTracker: procTracker,
  }
}

func (self *IIFDev) setProcTracker( procTracker ProcTracker ) {
  self.procTracker = procTracker
}

func (self *IIFDev) tunnel( pairs []TunPair ) {
  specs := []string{}
  for _,pair := range pairs {
    specs = append( specs, fmt.Sprintf("%d:%d",pair.from,pair.to) )//pair.String() )
    fmt.Printf("Tunnel from %d to %d\n", pair.from, pair.to )
  }
  args := []string {
    "tunnel",
    "-id", self.udid,
  }
  args = append( args, specs... )
  fmt.Printf("Starting %s with %s\n", self.bridge.cli, args )
  c := exec.Command( self.bridge.cli, args... )
  c.Stderr = os.Stderr
  c.Stdout = os.Stdout
  go func() {
    c.Run()
    fmt.Printf("Tunnel stopped\n")
  }()
  time.Sleep( time.Second * 2 )
}

func (self *IIFDev) info( names []string ) map[string]string {
  mapped := make( map[string]string )
  fmt.Printf("udid for info: %s\n", self.udid )
  args := []string {
    "info",
    "-json",
    "-id", self.udid,
  }
  args = append( args, names... )
  json, _ := exec.Command( self.bridge.cli, args... ).Output()
  fmt.Printf("json:%s\n",json)
  root, _ := uj.Parse( json )
  
  for _,name := range names {
    node := root.Get(name)
    if node != nil {
      mapped[name] = node.String()
    }
  }
  fmt.Printf("mapped result:%s\n",mapped)
  
  return mapped
}

func (self *IIFDev) gestalt( names []string ) map[string]string {
  mapped := make( map[string]string )
  args := []string{
    "mg",
    "-json",
    "-id", self.udid,
  }
  args = append( args, names... )
  fmt.Printf("Running %s %s\n", self.bridge.cli, args );
  json, _ := exec.Command( self.bridge.cli, args... ).Output()
  fmt.Printf("json:%s\n",json)
  root, _ := uj.Parse( json )
  for _,name := range names {
    node := root.Get(name)
    if node != nil {
      mapped[name] = node.String()
    }
  }
  
  return mapped
}

func (self *IIFDev) screenshot() Screenshot {
  return Screenshot{}
}

func (self *IIFDev) wdanew( xctestPath string, onStart func(), onStop func( interface{} ) ) {
  o := ProcOptions{
    procName: "xctest",
    binary: self.bridge.cli,
    args: []string{
      "xctest",
      xctestPath,
    },
    stdoutHandler: func( line string, plog *log.Entry ) {
        //if debug {
        //    fmt.Printf("[WDA] %s\n", line)
        //}
        if strings.HasPrefix(line, "Test Case '-[UITestingUITests testRunner]' started") {
            onStart()
        }
        if strings.Contains( line, "configuration is unsupported" ) {
            plog.Println( line )
        }
    },
    stderrHandler: func( line string, plog *log.Entry ) {
        if strings.Contains( line, "configuration is unsupported" ) {
            plog.Println( line )
        }
        //plog.Println( line )
    },
    onStop: func( wrapper interface{} ) {
      onStop( wrapper )
    },    
  }
      
  proc_generic( self.procTracker, nil, &o )
}

func (self *IIFDev) wda( xctestPath string, port int, onStart func(), onStop func(interface{}) ) {
  o := ProcOptions {
      procName: "wda",
      binary: "xcodebuild",
      startDir: "./bin/wda",
      args: []string{
          "test-without-building",
          "-xctestrun", xctestPath,
          "-destination", "id="+self.udid,
      },
      startFields: log.Fields{
          "testrun": xctestPath,
      },
      stdoutHandler: func( line string, plog *log.Entry ) {
          if strings.HasPrefix(line, "Test Case '-[UITestingUITests testRunner]' started") {
              //plog.Println("[WDA] successfully started")
              plog.WithFields( log.Fields{
                  "type": "wda_start",
                  "uuid": censorUuid(self.udid),
                  "port": port,
              } ).Info("[WDA] successfully started")
              onStart()
          }
          if strings.Contains( line, "configuration is unsupported" ) {
              plog.Println( line )
          }
      },
      stderrHandler: func( line string, plog *log.Entry ) {
          if strings.Contains( line, "configuration is unsupported" ) {
              plog.Println( line )
          }
      },
      onStop: func( wrapper interface{} ) {
          onStop( wrapper )
      },
  }
  
  proc_generic( self.procTracker, nil, &o )
}

func (self *IIFDev) destroy() {
  // close running processes
}