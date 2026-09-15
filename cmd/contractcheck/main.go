package main

import (
	"flag"
	"fmt"
	"os"

	"nadir/internal/contractcheck"
)

func main() {
	openAPI := flag.String("openapi", "contracts/http/openapi.yaml", "canonical OpenAPI contract")
	typescript := flag.String("typescript", "web/dashboard/src/lib/api-contract.ts", "central TypeScript contract mirror")
	flag.Parse()
	if err := contractcheck.Check(*openAPI, *typescript); err != nil {
		fmt.Fprintln(os.Stderr, "contract drift:", err)
		os.Exit(1)
	}
	fmt.Printf("contract check passed: %s matches %s\n", *typescript, *openAPI)
}
