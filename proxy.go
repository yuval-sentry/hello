package main

import (
	"fmt"
    "io"
	"io/ioutil"
	"net/http"
    "net/url"
    "bytes"
    "regexp"
    //"net/http/httputil"
    //"context"
    //"time"
    //"log"
	// "encoding/hex"
	// "errors"
    // "strconv"
    //"compress/zlib"
    "compress/gzip"
	//"encoding/base64"
    //"encoding/json"
    "strings"
)

const Port = ":8080"

type RequestTask struct {
    writer http.ResponseWriter
    request *http.Request
    //body   []byte
    done chan bool
}

// Channel to forward request details
var requestChan = make(chan RequestTask)

// Number of worker goroutines
const numWorkers = 15

// Tesing Component name to DSN MAP
//const tagName = "sentry_relay_component"
const componentNamePattern = `"sentry_relay_component":"([^"]+)"`

func constructSentryURL(dsn string, sentryAuth string) string {
    fmt.Println(sentryAuth)

    reKey := regexp.MustCompile(`sentry_key=([\w]+)`)
	reVersion := regexp.MustCompile(`sentry_version=([\w]+)`)
	reClient := regexp.MustCompile(`sentry_client=([\w/.]+)`)

	sentryKey := reKey.FindStringSubmatch(sentryAuth)[1]
	sentryVersion := reVersion.FindStringSubmatch(sentryAuth)[1]
	sentryClient := reClient.FindStringSubmatch(sentryAuth)[1]

    
    // Split the string based on the "@" symbol
	var url string = ""
    var publicKey string = ""
    var hostPathProject = ""
    var hostPath string = ""
    var projectId string = ""
    var endPoint string = "envelope"

    parts := strings.Split(dsn, "//")
    url = parts[1]
    parts = strings.Split(url, "@")
    publicKey = parts[0]
    hostPathProject = parts[1]
    parts = strings.Split(hostPathProject, "/")
    hostPath = parts[0]
    projectId = parts[1]

    baseURI := "https://" + hostPath

    sentryURL := baseURI + "/api/" + projectId + "/" + endPoint + "/?sentry_key=" + publicKey + "&sentry_version=" + sentryVersion + "&sentry_client=" + sentryClient + "&sentry_secret=" + sentryKey

    fmt.Println("sentryURL", sentryURL)

    return sentryURL
}

func getSentryAuth(req *http.Request) string {
    return req.Header.Get("X-Sentry-Auth")
}

// removeHeaders modifies the request to remove specific headers
func ModifyRequestHeaders(req *http.Request) {
    // List of headers to remove
    headersToRemove := []string{"X-Sentry-Auth"}

    for _, header := range headersToRemove {
        req.Header.Del(header)
    }
}

func ForwardRequest(w http.ResponseWriter, req *http.Request, target string) {
	targetURL, err := url.Parse(target)
	if err != nil {
		http.Error(w, "Failed to parse target URL", http.StatusInternalServerError)
		return
	}

    //fmt.Println(targetURL)

	// Modify the request URL to the target URL
	req.URL.Scheme = targetURL.Scheme
	req.URL.Host = targetURL.Host
	req.RequestURI = ""
	req.Host = targetURL.Host

	// Create a new request based on the original request with the modified headers
	newReq, err := http.NewRequest(req.Method, target, req.Body)
	if err != nil {
		http.Error(w, "Failed to create new request", http.StatusInternalServerError)
		return
	}
	newReq.Header = req.Header

	// Use httputil to copy the body and headers
	if req.Body != nil {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		newReq.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

    fmt.Printf("Entire new request object:   ")
    fmt.Printf("%#v\n", newReq)
    fmt.Printf("\n\n")

	// Send the modified request to the target server
	client := &http.Client{}
	resp, err := client.Do(newReq)
	if err != nil {
		http.Error(w, "Failed to forward request", http.StatusInternalServerError)
		return
	}
    fmt.Println("Forwarded request response", resp)
	defer resp.Body.Close()

	// Copy the response headers and body to the original response writer
	// for key, values := range resp.Header {
	// 	for _, value := range values {
	// 		w.Header().Add(key, value)
	// 	}
	// }
	// w.WriteHeader(resp.StatusCode)
	// io.Copy(w, resp.Body)
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

    sentryAuth := getSentryAuth(task.request)
    targetURL := constructSentryURL(componentToDSN[componentName], sentryAuth)
    ModifyRequestHeaders(task.request)
    header := task.request.Header.Get("X-Sentry-Auth")
    fmt.Println("checking sentry auth header value", header)
    ForwardRequest(task.writer, task.request, targetURL)

    fmt.Printf("\n\n\n\n\n\n\n\n")
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
            writer: w,
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