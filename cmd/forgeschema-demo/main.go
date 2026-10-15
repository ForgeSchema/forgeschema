package main

import (
	"context"
	"github.com/ForgeSchema/forgeschema/internal/demo"
	"os"
)

func main() { os.Exit(demo.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }
