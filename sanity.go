package main

import (
    "fmt"
    "os"
    "path/filepath"
    uc "github.com/nanoscopic/uclop/mod"
)

func sanityChecks( config *Config, cmd *uc.Cmd ) bool {
    // Verify iosif has been built / exists
    bridge := config.bridge
    
    if bridge == "go-ios" {
        goIosPath := config.goIosPath
        goIosPath, _ = filepath.Abs( goIosPath )
        if _, err := os.Stat( goIosPath ); os.IsNotExist( err ) {
            fmt.Fprintf(os.Stderr,"%s does not exist. Rerun `make` to build go-ios\n",goIosPath)
            return false
        }
    }
    
    if bridge == "iosif" {
        iosIfPath := config.iosIfPath
        iosIfPath, _ = filepath.Abs( iosIfPath )
        if _, err := os.Stat( iosIfPath ); os.IsNotExist( err ) {
            fmt.Fprintf(os.Stderr,"%s does not exist. Rerun `make` to build iosif\n",iosIfPath)
            return false
        }
    }
    
    /*if config.wdaSanityCheck {
        wdaPath := config.wdaPath
        wdaPath, _ = filepath.Abs( "./" + wdaPath )
        if _, err := os.Stat( cfaPath ); os.IsNotExist( err ) {
            fmt.Fprintf(os.Stderr,"%s does not exist. Rerun `make` to build WebDriverAgent\n",wdaPath)
            return false
        }
    }*/
    
    if config.cfaSanityCheck {
        cfaPath := config.cfaPath
        cfaPath, _ = filepath.Abs( "./" + cfaPath )
        if _, err := os.Stat( cfaPath ); os.IsNotExist( err ) {
            fmt.Fprintf(os.Stderr,"%s does not exist. Rerun `make` to build CFAgent\n",cfaPath)
            return false
        }
    }
    
    return true
}
