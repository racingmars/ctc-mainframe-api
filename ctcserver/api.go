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
	"net/http"

	"github.com/labstack/echo/v4"

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

func (app *api) quit(c echo.Context) error {
	err := app.ctcapi.Quit()
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			errorResponse{Error: err.Error()})
	}

	return c.NoContent(http.StatusOK)
}
