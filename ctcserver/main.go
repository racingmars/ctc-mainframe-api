package main

// Copyright 2022-2023 Matthew R. Wilson <mwilson@mattwilson.org>
//
// This file is part of CTC Mainframe API. CTC Mainframe API is free software:
// you can redistribute it and/or modify it under the terms of the GNU General
// Public License as published by the Free Software Foundation, either version
// 3 of the license, or (at your option) any later version.
//
// https://github.com/racingmars/ctc-mainframe-api/

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/racingmars/ctc-mainframe-api/ctcserver/internal/ctc"
	"github.com/racingmars/ctc-mainframe-api/ctcserver/internal/ctcapi"
)

func main() {
	flagDebug := flag.Bool("debug", false, "Enable debug logging")
	flagTrace := flag.Bool("trace", false, "Enable trace logging")
	flagPretty := flag.Bool("pretty", false, "Enable pretty logging")
	flagConfig := flag.String("config", "config.json", "Config file path")
	flagCodepage := flag.String("codepage", "bracket",
		"Code page - 'bracket' or 'cp37'")

	fmt.Println()
	fmt.Println("CTC Mainframe API")
	fmt.Println("Copyright 2022 Matthew R. Wilson <mwilson@mattwilson.org>")
	fmt.Println("https://github.com/racingmars/ctc-mainframe-api/")
	fmt.Println()
	fmt.Println("This program comes with ABSOLUTELY NO WARRANTY.")
	fmt.Println()
	fmt.Println("This is free software, and you are welcome to redistribute it")
	fmt.Println("and/or modify it under the terms of the GNU General Public")
	fmt.Println("License as published by the Free Software Foundation, either")
	fmt.Println("version 3 of the License, or (at your option) any later")
	fmt.Println("version.")
	fmt.Println()

	flag.Parse()

	if *flagPretty {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// -trace is higher precedence than -debug
	if *flagTrace {
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
		log.Trace().Msg("trace logging enabled")
	} else if *flagDebug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Debug().Msg("debug logging enabled")
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Set up selected codepage globally. Eventually this should probably be
	// per-API-call.
	switch *flagCodepage {
	case "bracket":
		ctc.SetGlobalCodepage(ctc.CodepageBracket)
	case "cp37":
		ctc.SetGlobalCodepage(ctc.CodepageCP37)
	default:
		log.Error().Msgf("-codepage parameter must be \"bracket\" or "+
			"\"cp37\". \"%s\" is not supported.", *flagCodepage)
		os.Exit(1)
	}

	if i := realMain(*flagConfig); i > 0 {
		os.Exit(i)
	}
}

// we wrap most of "main" in a realMain() function that returns an exit code.
// This allows the real main() function to use os.Exit() if necessary, but
// returning from realMain() allows any defers to still run.
func realMain(configPath string) int {

	config, err := readConfig(configPath)
	if err != nil {
		log.Error().Err(err).Msg("couldn't read server configuration")
		return 1
	}

	// Get our CTC command and data emulated devices
	ctccmd, ctcdata, err := connect(config)
	if err != nil {
		log.Error().Err(err).Msg("unable to connect to Hercules")
		return 1
	}

	defer ctccmd.Close()
	defer ctcdata.Close()

	// ...and use them for our CTC API
	capi := ctcapi.New(ctccmd, ctcdata)
	app := api{
		ctcapi: capi,
	}

	// Set up the echo HTTP service
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.CORS())
	e.Use(middleware.Logger())

	// Add our API endpoints
	e.GET("/api/dslist/:prefix", app.dslist)
	e.GET("/api/mbrlist/:pdsName", app.mbrlist)
	e.GET("/api/read/:dsn", app.read)
	e.POST("/api/submit", app.submit)
	e.POST("/api/write/:dsn", app.write)
	e.GET("/api/quit", app.quit)

	// Run it
	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", config.ListenPort)))

	return 0
}

func connect(config configuration) (ctccmd, ctcdata ctc.CTC, err error) {
	var hercVer ctc.HerculesVersion
	var byteOrder binary.ByteOrder

	if config.Hercules313 {
		hercVer = ctc.HerculesVersionOld
	} else {
		hercVer = ctc.HerculesVersionNew
	}

	if config.HerculesHostBigEndian {
		byteOrder = binary.BigEndian
	} else {
		byteOrder = binary.LittleEndian
	}

	// We need to listen for both CTC connections simultaneously. This is
	// particularly important for Hercules 3.13, which has absolutely no
	// re-connection logic, unlike Spinhawk and later. We'll fire both
	// connection attempts off in goroutines and wait for them.

	var wg sync.WaitGroup
	var connectError bool
	wg.Add(2)

	// Establish the command device connection.
	go func() {
		defer wg.Done()
		var err error // don't race on err from the outer function
		ctccmd, err = ctc.New(config.CmdLPort, config.CmdRPort, 0x500,
			config.HerculesHost, hercVer, byteOrder)
		if err != nil {
			connectError = true
			log.Error().Err(err).Msg(
				"couldn't create CTC command device connection")
			return
		}

		if err := ctccmd.Connect(); err != nil {
			connectError = true
			log.Error().Err(err).Msg("couldn't create CTC command device")
			return
		}
	}()

	// Establish the data device connection.
	go func() {
		defer wg.Done()
		var err error // don't race on err from the outer function
		ctcdata, err = ctc.New(config.DataLPort, config.DataRPort, 0x501,
			config.HerculesHost, hercVer, byteOrder)
		if err != nil {
			connectError = true
			log.Error().Err(err).Msg(
				"couldn't create CTC data device connection")
			return
		}

		if err := ctcdata.Connect(); err != nil {
			connectError = true
			log.Error().Err(err).Msg("couldn't connect CTC data device")
			return
		}
	}()

	wg.Wait()
	if connectError {
		// We don't know which connection failed, so we'll be careful during
		// cleanup.
		if ctccmd != nil {
			ctccmd.Close()
		}
		if ctcdata != nil {
			ctcdata.Close()
		}
		return nil, nil, fmt.Errorf("couldn't connect to Hercules")
	}

	return ctccmd, ctcdata, nil
}
