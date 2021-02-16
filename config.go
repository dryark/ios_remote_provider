package main

import (
    "fmt"
    "io/ioutil"
    "os"
    uj "github.com/nanoscopic/ujsonin/mod"
    log "github.com/sirupsen/logrus"
)

type CDevice struct {
    udid string
}

type Config struct {
    iosIfPath      string
    httpPort           int
    cfHost             string
    cfUsername         string
    devs               map [string] CDevice
    xcPath             string
}

func NewConfig( configPath string, defaultsPath string ) (*Config) {
    config := Config{}
    
    root := loadConfig( configPath, defaultsPath )
    
    binPaths := root.Get("bin_paths")
    if binPaths == nil {
        fmt.Fprintf(os.Stderr,"bin_paths is not set in config.json")
        os.Exit(1)
    }
    iosifNode := binPaths.Get("iosif")
    if iosifNode == nil {
        fmt.Fprintf(os.Stderr,"iosif is not set in config.json")
        os.Exit(1)
    }
    config.iosIfPath = iosifNode.String()
        
    portNode := root.Get("port")
    if root == nil {
        fmt.Fprintf(os.Stderr,"port is not set in config.json")
        os.Exit(1)
    }
    config.httpPort = portNode.Int()
    
    cfNode := root.Get("controlfloor")
    if cfNode == nil {
        fmt.Fprintf(os.Stderr,"controlfloor is not set in config.json")
        os.Exit(1)
    }
    cfHostNode := cfNode.Get("host")
    if cfHostNode == nil {
        fmt.Fprintf(os.Stderr,"host is not set in config.json")
        os.Exit(1)
    }
    config.cfHost = cfHostNode.String()
    
    cfIdNode := cfNode.Get("username")
    if cfIdNode == nil {
        fmt.Fprintf(os.Stderr,"username is not set in config.json")
        os.Exit(1)
    }
    config.cfUsername = cfIdNode.String()
    
    xcNode := root.Get("wdaXctestRunFolder")
    if xcNode == nil {
        fmt.Fprintf(os.Stderr,"wdaXctestRunFolder is not set in config.json")
        os.Exit(1)
    }
    config.xcPath = xcNode.String()
    
    config.devs = readDevs( root )
    
    return &config
}

func readDevs( root *uj.JNode ) ( map[string]CDevice ) {
    devs := make( map[string]CDevice )
    
    devsNode := root.Get("devices")
    if devsNode != nil {
        devsNode.ForEach( func( devNode *uj.JNode ) {
            udid := devNode.Get("udid").String()
                        
            dev := CDevice{
                udid: udid,
            }
            devs[ udid ] = dev
        } )
    }
    return devs
}

func loadConfig( configPath string, defaultsPath string ) (*uj.JNode) {
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
    defaults.Dump()
    
    return defaults
}