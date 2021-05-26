-include config.mk

TARGET = main

all: $(TARGET) bin/iosif repos/vidapp bin/go-ios

bin/gojq: repos/ujsonin/versionMarker
	make -C repos/ujsonin gojq && touch bin/gojq

bin/go-ios: repos/go-ios/versionMarker
	cd repos/go-ios && go build .
	touch bin/go-ios

config.mk: config.json bin/gojq
	@rm -rf config.mk
	@./bin/gojq makevars -prefix config -file config.json -defaults default.json -outfile config.mk

provider_sources := $(wildcard *.go)

@if [ "$(PROD_PATH)" != "" ]; then cp -r $(PROD_PATH)/ bin/wda/; fi;

$(TARGET): config.mk $(provider_sources) go.mod
	@if [ "$(config_jsonfail)" == "1" ]; then\
		echo $(config_jsonerr) ;\
		exit 1;\
	fi
	go build -o $(TARGET) -tags macos .

go.sum:
	go get
	go get .

clean:
	$(RM) $(TARGET) go.sum

wdaclean:
	$(RM) -rf repos/WebDriverAgent/build

repos/WebDriverAgent/versionMarker: repos/WebDriverAgent repos/versionMarkers/WebDriverAgent
	cd repos/WebDriverAgent && git pull
	touch repos/WebDriverAgent/versionMarker

repos/WebDriverAgent:
	git clone $(config_repos_wda) repos/WebDriverAgent

repos/ujsonin/versionMarker: repos/ujsonin repos/versionMarkers/ujsonin
	cd repos/ujsonin && git pull
	touch repos/ujsonin/versionMarker

repos/ujsonin:
	git clone https://github.com/nanoscopic/ujsonin.git repos/ujsonin
	touch repos/ujsonin/versionMarker

bin/iosif: repos/iosif/versionMarker
	make -C repos/iosif
	touch bin/iosif

repos/iosif/versionMarker: repos/iosif repos/versionMarkers/iosif
	cd repos/iosif && git pull
	touch repos/iosif/versionMarker

repos/iosif:
	git clone $(config_repos_iosif) repos/iosif

repos/vidapp:
	git clone $(config_repos_vidapp) repos/vidapp

repos/go-ios/versionMarker: repos/go-ios repos/versionMarkers/go-ios
	cd repos/go-ios && git pull
	touch repos/go-ios/versionMarker

repos/go-ios:
	git clone $(config_repos_goios) repos/go-ios

vidtest_unsigned.xcarchive:
	xcodebuild -project repos/vidapp/vidtest/vidtest.xcodeproj -scheme vidtest archive -archivePath ./vidtest.xcarchive CODE_SIGN_IDENTITY="" CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=NO

vidtest.xcarchive: repos/vidapp
	@./bin/gojq overlay -file1 default.json -file2 config.json -json > muxed.json
	./python/configure_vidtest.py muxed.json
	xcodebuild -project repos/vidapp/vidtest/vidtest.xcodeproj -scheme vidtest archive -archivePath ./vidtest.xcarchive

vidtest.ipa: vidtest.xcarchive repos/vidapp
	plutil -replace teamID -string $(config_vidtest_devTeamOu) ./repos/vidapp/vidtest/ExportOptions.plist
	@if [ -e vidtest.ipa ]; then rm vidtest.ipa; fi
	xcodebuild -exportArchive -archivePath ./vidtest.xcarchive -exportOptionsPlist ./repos/vidapp/vidtest/ExportOptions.plist -exportPath vidtest.ipa

vidtest.ipa/vidtest.ipa_x: vidtest.ipa
	mkdir vidtest.ipa/vidtest.ipa_x
	unzip vidtest.ipa/vidtest.ipa -d vidtest.ipa/vidtest.ipa_x
	find vidtest.ipa/vidtest.ipa_x | grep provision | xargs rm
	find vidtest.ipa/vidtest.ipa_x | grep _CodeSignature$$ | xargs rm -rf

vidtest_clean.ipa: vidtest.ipa/vidtest.ipa_x
	cd vidtest.ipa/vidtest.ipa_x && zip -r ../../vidtest_clean.ipa Payload

installvidapp: vidtest.xcarchive
	ios-deploy -b vidtest.xcarchive/Products/Applications/vidtest.app

vidtest_unsigned.ipa:
	@if [ -e tmp ]; then rm -rf tmp; fi
	mkdir tmp
	mkdir tmp/Payload
	ln -s ../../vidtest.xcarchive/Products/Applications/vidtest.app tmp/Payload/vidtest.app
	cd tmp && zip -r ../vidtest.ipa Payload

clonewda: repos/WebDriverAgent

wda: repos/WebDriverAgent/build

python/deps_installed: repos/mod-pbxproj
	pip3 install -r ./python/requires.txt
	touch python/deps_installed

repos/WebDriverAgent/build: repos/WebDriverAgent/versionMarker repos/mod-pbxproj config.json python/deps_installed bin/gojq
	@if [ -e repos/WebDriverAgent/build ]; then rm -rf repos/WebDriverAgent/build; fi;
	mkdir repos/WebDriverAgent/build
	@./bin/gojq overlay -file1 default.json -file2 config.json -json > muxed.json
	./python/configure_wda.py muxed.json
	cd repos/WebDriverAgent && xcodebuild -scheme WebDriverAgentRunner -allowProvisioningUpdates -destination generic/platform=iOS -derivedDataPath "./build" build-for-testing

repos/mod-pbxproj:
	git clone $(config_repos_pbxproj) repos/mod-pbxproj

usetidevice: calculated.json

calculated.json: bin/gojq repos/versionMarkers/calculated_json
	$(eval TIDEVICE_PKGS_PATH := $(shell pip3 show tidevice | grep Location | cut -c 11-))
	$(eval TIDEVICE_BIN_PATH := $(shell pip3 show -f tidevice | grep bin/tidevice))
	@./bin/gojq set -file calculated.json -path tidevice -val $(TIDEVICE_PKGS_PATH)/$(TIDEVICE_BIN_PATH)
	