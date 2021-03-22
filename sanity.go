package main

import (
    "fmt"
    "os"
    "path/filepath"
    uc "github.com/nanoscopic/uclop/mod"
)

func sanityChecks( config *Config, cmd *uc.Cmd ) bool {
    // Verify iosif has been built / exists  
    iosIfPath := config.iosIfPath
    iosIfPath, _ = filepath.Abs( iosIfPath )
    if _, err := os.Stat( iosIfPath ); os.IsNotExist( err ) {
        fmt.Fprintf(os.Stderr,"%s does not exist. Rerun `make` to build iosif\n",iosIfPath)
        return false
    }
    
    wdaPath := config.wdaPath
    wdaPath, _ = filepath.Abs( "./" + wdaPath )
    if _, err := os.Stat( wdaPath ); os.IsNotExist( err ) {
        fmt.Fprintf(os.Stderr,"%s does not exist. Rerun `make` to build WebDriverAgent\n",wdaPath)
        return false
    }
    
    return true
}