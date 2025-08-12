# Summary
This project is inspired by https://github.com/intel/ISALgo
# Prerequisites
ISA-l

# How to build
If ISA-l is not in a standard Linux path, please replace the paths accordingly.
```
CGO_CFLAGS='-I/opt/isal-l/include -L/opt/isal-l/lib -lisa' CGO_LDFLAGS='-L/opt/isal-l/lib' go build
```

# examples

## isal.DecompressCopy
```
package main

import (
	"fmt"
	"log"
	"os"

	isal "github.com/zjj/ISALgo2/v2"
)

func main() {
	// Check if a filename was provided
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <gzip_file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s file.txt.gz\n", os.Args[0])
		os.Exit(1)
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	isal.DecompressCopy(file, os.Stdout)
}

```

## isal.Reader
```
package main

import (
	"fmt"
	"log"
	"os"

	isal "github.com/zjj/ISALgo2/v2"
)

func main() {
	// Check if a filename was provided
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <gzip_file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s file.txt.gz\n", os.Args[0])
		os.Exit(1)
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	reader, err := isal.NewReader(file)
	if err != nil {
		log.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer reader.Close()

	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
		}
		if err != nil {
			if err.Error() != "EOF" {
				log.Fatalf("Read error: %v", err)
			}
			break
		}
	}

}

```

## isal.Writer / isal.NewWriterLevel / isal.CompressCopyLevel

### Example: Using isal.Writer (default compression)
```go
package main

import (
	"bytes"
	"fmt"
	"os"

	isal "github.com/zjj/ISALgo2/v2"
)

func main() {
	input := []byte("hello, isal!")
	var compressed bytes.Buffer

	w, err := isal.NewWriter(&compressed)
	if err != nil {
		panic(err)
	}
	w.Write(input)
	w.Close()

	fmt.Printf("Compressed %d bytes to %d bytes\n", len(input), compressed.Len())
}
```

### Example: Using isal.NewWriterLevel (custom compression level)
```go
package main

import (
	"bytes"
	"fmt"
	"os"

	isal "github.com/zjj/ISALgo2/v2"
)

func main() {
	input := []byte("compress with best compression!")
	var compressed bytes.Buffer

	w, err := isal.NewWriterLevel(&compressed, isal.BestCompression)
	if err != nil {
		panic(err)
	}
	w.Write(input)
	w.Close()

	fmt.Printf("Compressed %d bytes to %d bytes\n", len(input), compressed.Len())
}
```

### Example: Using isal.CompressCopyLevel (streaming, file-to-file)
```go
package main

import (
	"log"
	"os"

	isal "github.com/zjj/ISALgo2/v2"
)

func main() {
	in, err := os.Open("input.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer in.Close()

	out, err := os.Create("output.txt.gz")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	if err := isal.CompressCopyLevel(in, out, isal.DefaultCompression); err != nil {
		log.Fatal(err)
	}
}
```
