package main

import (
	"fmt"
    "io"
	"io/ioutil"
	"net/http"
    "net/url"
    "bytes"
    "regexp"
    "net/http/httputil"
    "context"
    //"log"
	// "encoding/hex"
	// "errors"
    // "strconv"
    //"compress/zlib"
    "compress/gzip"
	//"encoding/base64"
    //"encoding/json"
    //"strings"
)

const Port = ":8080"

type RequestTask struct {
    response http.ResponseWriter
    request *http.Request
    //body   []byte
    done chan bool
}

type loggingResponseWriter struct {
    http.ResponseWriter
    statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(statusCode int) {
    lrw.statusCode = statusCode
    lrw.ResponseWriter.WriteHeader(statusCode)
}

// Channel to forward request details
var requestChan = make(chan RequestTask)

// Number of worker goroutines
const numWorkers = 15

// Tesing Component name to DSN MAP
//const tagName = "sentry_relay_component"
const componentNamePattern = `"sentry_relay_component":"([^"]+)"`

func constructSentryURL(dsn string) {

}

// Worker function to process requests
func worker(task RequestTask) {
    var json string = ""
    var componentName string = ""
    var componentToDSN = make(map[string]string)
    componentToDSN["A"] = "https://133008b01af043a021a841977bf7daae@o87286.ingest.us.sentry.io/4507274024058880"
    componentToDSN["B"] = "https://b338268c4c61a9d5096d311a432f2979@o87286.ingest.us.sentry.io/4507274029236224"
    //componentToDSN["B"] = "https://o87286.ingest.us.sentry.io"
    //var defaultDSN string = "https://efe273e1f9aae6f6f0bc4fb089fab1d7@o87286.ingest.us.sentry.io/4507262272208896"
   
    //for task := range tasks {
        // Process request (e.g., log or handle asynchronously)
        fmt.Printf("\n\n\n\n\n\nReceived request: %s %s\n", task.request.Method, task.request.URL)

        for key, values := range task.request.Header {
            for _, value := range values {
                fmt.Printf("\t%s: %s\n", key, value)
            }
        }

        // Read request body
        body, err := io.ReadAll(task.request.Body)
        if err != nil {
            fmt.Println("Failed to read request body", err)
        }
        task.request.Body.Close()
        //fmt.Println("Received body %s\n", string(body))

        reader := bytes.NewReader(body)

        //Create a gzip reader to decompress the data
        gzipReader, err := gzip.NewReader(reader)
        //defer gzipReader.Close()
         if err != nil {
             fmt.Printf("Body received raw, not gzipped")
             fmt.Printf("Body: %s\n", string(body))
             json = string(body)
             //return
        } else {

             // Read the decompressed data
             decompressedData, err := ioutil.ReadAll(gzipReader)
            if err != nil {
                fmt.Println("Error reading decompressed data:", err)
                //return
            }
            fmt.Println("Decompressed data:", string(decompressedData))
            json = string(decompressedData)
        }

        re := regexp.MustCompile(componentNamePattern)
        matches := re.FindStringSubmatch(json)
        
        if len(matches) > 1 {
            // The first submatch (index 1) will be the content of the capturing group
            componentName = matches[1]
            fmt.Println("Extracted value:", componentName)
        } else {
            fmt.Println("No match found")
        }

        //Create a reverse proxy
        //Prepare the traget URL
        
        
        targetURL, err := url.Parse(componentToDSN[componentName])
        if err != nil {
            fmt.Println("Failed to parse target URL: %v", err)
        }

        
        fmt.Println("TargetURL: \n", targetURL)

        // Create the reverse proxy
        proxy := httputil.NewSingleHostReverseProxy(targetURL)


        proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
            if r.Context().Err() == context.Canceled {
                fmt.Println("Client disconnected")
                return
            }
            http.Error(w, "Proxy error", http.StatusBadGateway)
        }
        lrw := &loggingResponseWriter{ResponseWriter: task.response}
        proxy.ServeHTTP(lrw, task.request)
        fmt.Printf("Received status code: %d\n", lrw.statusCode)

        fmt.Printf("\n\n\n\n\n\n\n\n")

        // // Modify the request before forwarding
        // proxy.ModifyRequest = func(req *http.Request) {
        //     // Add or modify headers and json
        //     req.Header.Set("X-Forwarded-For", req.RemoteAddr)
        // }


            // Print the decompressed data as a string
            
        //     //json = decompressedData

        //     convertStringToJsonObject(string(decompressedData))
        //     //fmt.Println("jsonObject ", jsonObject)
        //     // Remove all whitespace using strings.ReplaceAll()
        //     //result := strings.ReplaceAll(string(decompressedData), " ", "")

        //     // Print the result
        //     //fmt.Println("String with spaces removed:", result)
        // }
    //}
}

func main() {
    // Start worker goroutines
    // for i := 0; i < numWorkers; i++ {
    //     go worker(i, requestChan)
    // }

    go func() {
        for task := range requestChan {
            worker(task)
            task.done <- true
        }
    }()


    // Register handler function for the root URL pattern "/"
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        task := RequestTask{
            request: r,
            response: w,
            done: make(chan bool),
        }
        requestChan <- task
        <-task.done
    })

    // Start the HTTP server on "$Port"
    fmt.Println("Server listening on port 8080" + Port)
    if err := http.ListenAndServe(Port, nil); err != nil {
        fmt.Printf("Failed to start server: %s\n", err)
    }
}