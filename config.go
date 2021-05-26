package main

import (
    "crypto/tls"
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    uj "github.com/nanoscopic/ujsonin/v2/mod"
    log "github.com/sirupsen/logrus"
)

type CDevice struct {
    udid string
    uiWidth int
    uiHeight int
    controlCenterMethod string
}

type Config struct {
    iosIfPath    string
    httpPort     int
    cfHost       string
    cfUsername   string
    devs         map [string] CDevice
    xcPath       string
    https        bool
    selfSigned   bool
    wdaPath      string
    tidevicePath string
    wdaMethod    string
    wdaPrefix    string
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

func NewConfig( configPath string, defaultsPath string, calculatedPath string ) (*Config) {
    config := Config{}
    
    root := loadConfig( configPath, defaultsPath, calculatedPath )
    
    config.iosIfPath  = GetStr(  root, "bin_paths.iosif" )
    config.httpPort   = GetInt(  root, "port" )
    config.cfHost     = GetStr(  root, "controlfloor.host" )
    config.cfUsername = GetStr(  root, "controlfloor.username" )
    config.xcPath     = GetStr(  root, "wdaXctestRunFolder" )
    config.https      = GetBool( root, "controlfloor.https" )
    config.selfSigned = GetBool( root, "controlfloor.selfSigned" )
    config.wdaPath    = GetStr(  root, "bin_paths.wda" )
    config.wdaMethod  = GetStr(  root, "wda.startMethod" )
    config.wdaPrefix  = GetStr(  root, "wda.bundleIdPrefix" )
    tideviceNode := root.Get( "tidevice" )
    if tideviceNode != nil {
        config.tidevicePath = tideviceNode.String()
    } else {
        config.tidevicePath = ""
    }
    
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
            uiWidth := 0
            uiHeight := 0
            controlCenterMethod := "bottomUp"
            widthNode := devNode.Get("uiWidth")
            if widthNode != nil {
              uiWidth = widthNode.Int()
            }
            heightNode := devNode.Get("uiHeight")
            if heightNode != nil {
              uiHeight = heightNode.Int()
            }
            methodNode := devNode.Get("controlCenterMethod")
            if methodNode != nil {
              controlCenterMethod = methodNode.String()
            }
            
            dev := CDevice{
                udid: udid,
                uiWidth: uiWidth,
                uiHeight: uiHeight,
                controlCenterMethod: controlCenterMethod,
            }
            devs[ udid ] = dev
        } )
    }
    return devs
}

func loadConfig( configPath string, defaultsPath string, calculatedPath string ) (uj.JNode) {
    // read in defaults
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
    
    // read in normal config
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
    
    if calculatedPath != "" {
        fh2, serr2 := os.Stat( calculatedPath )
        if serr2 != nil {
            log.WithFields( log.Fields{
                "type":        "err_read_calculated",
                "error":       serr2,
                "defaults_path": calculatedPath,
            } ).Fatal("Could not read specified calculated path")
        }
        calculatedFile := calculatedPath
        switch mode := fh2.Mode(); {
            case mode.IsDir(): calculatedFile = fmt.Sprintf("%s/default.json", calculatedPath)
        }
        content2, err2 := ioutil.ReadFile( calculatedFile )
        if err2 != nil { log.Fatal( err2 ) }
        calculated, _ := uj.Parse( content2 )
        defaults.Overlay( calculated )
    }
    //defaults.Dump()
    
    return defaults
}