package main

import (
    "crypto/tls"
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    uj "github.com/nanoscopic/ujsonin/mod"
    log "github.com/sirupsen/logrus"
)

type CDevice struct {
    udid string
}

type Config struct {
    iosIfPath  string
    httpPort   int
    cfHost     string
    cfUsername string
    devs       map [string] CDevice
    xcPath     string
    https      bool
    selfSigned bool
}

func GetStr( root uj.JNode, path string ) string {
    node := root.Get( path )
    if node == nil {
        fmt.Fprintf( os.Stderr, "%s is not set in either config.json or default.json" )
        os.Exit(1)
    }
    return node.String()
}
func GetBool( root uj.JNode, path string ) bool {
    node := root.Get( path )
    if node == nil {
        fmt.Fprintf( os.Stderr, "%s is not set in either config.json or default.json" )
        os.Exit(1)
    }
    return node.Bool()
}
func GetInt( root uj.JNode, path string ) int {
    node := root.Get( path )
    if node == nil {
        fmt.Fprintf( os.Stderr, "%s is not set in either config.json or default.json" )
        os.Exit(1)
    }
    return node.Int()
}

func NewConfig( configPath string, defaultsPath string ) (*Config) {
    config := Config{}
    
    root := loadConfig( configPath, defaultsPath )
    
    config.iosIfPath  = GetStr(  root, "bin_paths.iosif" )
    config.httpPort   = GetInt(  root, "port" )
    config.cfHost     = GetStr(  root, "controlfloor.host" )
    config.cfUsername = GetStr(  root, "controlfloor.username" )
    config.xcPath     = GetStr(  root, "wdaXctestRunFolder" )
    config.https      = GetBool( root, "controlfloor.https" )
    config.selfSigned = GetBool( root, "controlfloor.selfSigned" )
    
    if config.https {
        if config.selfSigned {
            http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
              InsecureSkipVerify: true,
            }
            //http.DefaultTransport.(*http.Transport).ForceAttemptHTTP2 = false
        }
    } 
    
    config.devs = readDevs( root )
    
    return &config
}

func readDevs( root uj.JNode ) ( map[string]CDevice ) {
    devs := make( map[string]CDevice )
    
    devsNode := root.Get("devices")
    if devsNode != nil {
        devsNode.ForEach( func( devNode uj.JNode ) {
            udid := devNode.Get("udid").String()
                        
            dev := CDevice{
                udid: udid,
            }
            devs[ udid ] = dev
        } )
    }
    return devs
}

func loadConfig( configPath string, defaultsPath string ) (uj.JNode) {
    fh1, serr1 := os.Stat( defaultsPath )
    if serr1 != nil {
        log.WithFields( log.Fields{
            "type":        "err_read_defaults",
            "error":       serr1,
            "defaults_path": defaultsPath,
        } ).Fatal("Could not read specified defaults path")
    }
    defaultsFile := defaultsPath
    switch mode := fh1.Mode(); {
        case mode.IsDir(): defaultsFile = fmt.Sprintf("%s/default.json", defaultsPath)
    }
    content1, err1 := ioutil.ReadFile( defaultsFile )
    if err1 != nil { log.Fatal( err1 ) }
  
    defaults, _ := uj.Parse( content1 )
    
    fh, serr := os.Stat( configPath )
    if serr != nil {
        log.WithFields( log.Fields{
            "type":        "err_read_config",
            "error":       serr,
            "config_path": configPath,
        } ).Fatal("Could not read specified config path")
    }
    configFile := configPath
    switch mode := fh.Mode(); {
        case mode.IsDir(): configFile = fmt.Sprintf("%s/config.json", configPath)
    }
    content, err := ioutil.ReadFile( configFile )
    if err != nil { log.Fatal( err ) }
  
    root, _ := uj.Parse( content )
    
    defaults.Overlay( root )
    //defaults.Dump()
    
    return defaults
}