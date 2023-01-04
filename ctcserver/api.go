package main

// Copyright 2022 Matthew R. Wilson <mwilson@mattwilson.org>
//
// This file is part of CTC Mainframe API. CTC Mainframe API is free software:
// you can redistribute it and/or modify it under the terms of the GNU General
// Public License as published by the Free Software Foundation, either version
// 3 of the license, or (at your option) any later version.
//
// https://github.com/racingmars/ctc-mainframe-api/

import (
	"bufio"
	"bytes"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"

	"github.com/racingmars/ctc-mainframe-api/ctcserver/internal/ctcapi"
)

type api struct {
	ctcapi ctcapi.CTCAPI
}

type errorResponse struct {
	Error string `json:"error"`
}

func (app *api) dslist(c echo.Context) error {
	prefix := c.Param("prefix")

	results, err := app.ctcapi.GetDSList(prefix)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			errorResponse{Error: err.Error()})
	}

	return c.JSON(http.StatusOK, results)
}

func (app *api) mbrlist(c echo.Context) error {
	pdsName := c.Param("pdsName")

	results, err := app.ctcapi.GetMemberList(pdsName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			errorResponse{Error: err.Error()})
	}

	return c.JSON(http.StatusOK, results)
}

func (app *api) read(c echo.Context) error {
	dsn := c.Param("dsn")
	ebcdicQueryParam := c.QueryParam("ebcdic")

	raw := false
	if ebcdicQueryParam == "true" {
		raw = true
	}

	results, err := app.ctcapi.Read(dsn, raw)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			errorResponse{Error: err.Error()})
	}

	// ASCII-translated output
	if !raw {
		var output strings.Builder
		for _, record := range results {
			output.WriteString(string(record))
			output.WriteString("\n")
		}
		return c.String(http.StatusOK, output.String())
	}

	// Raw binary output
	var output bytes.Buffer
	for _, record := range results {
		output.Write(record)
	}
	return c.Blob(http.StatusOK, "application/octet-stream", output.Bytes())

}

func (app *api) submit(c echo.Context) error {
	var records []string
	scanner := bufio.NewScanner(c.Request().Body)
	for scanner.Scan() {
		line := scanner.Text()
		log.Trace().Msgf("Scanned one JCL record: %s", line)
		records = append(records, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	result, err := app.ctcapi.Submit(records)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			errorResponse{Error: err.Error()})
	}

	return c.String(http.StatusOK, result)

}

func (app *api) quit(c echo.Context) error {
	err := app.ctcapi.Quit()
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			errorResponse{Error: err.Error()})
	}

	return c.NoContent(http.StatusOK)
}
