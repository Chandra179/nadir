package ingest

type Result struct {
	Processed int
	Skipped   int
	Failed    int
}

type Igest interface{}
