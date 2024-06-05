package main

import (
	"fmt"
    "io"
	"net/http"
    "bytes"
    "regexp"
    "compress/gzip"
    "strings"
    "encoding/json"
    "os"
    "net/url"
    "time"
    "strconv"
)

const Port = ":8080"

type RequestTask struct {
    Method string
    URL    string
    Body   []byte
    Host string
    RequestURI string
    Header map[string][]string
    Writer http.ResponseWriter
}

type Event struct {
    Tags map[string]string
}

var defaultDSN string = ""
var configFilePath  string = ""
// Number of worker goroutines
var numWorkers int = 15

type Config struct {
    Mapping map[string]string `json:"mapping"`
}

var ComponentToDSNMapping map[string]string

// Channel to forward request details
var requestChan = make(chan RequestTask)

const componentTagName = "sentry_relay_component"

// IsValidURL checks if a given URL string is valid.
func IsValidURL(testURL string) bool {
    _, err := url.ParseRequestURI(testURL)
    if err != nil {
        fmt.Println("IsValidURL :: " + testURL + " is invalid url ", err)
    }
    return err == nil
}

func IsValidDSN(testDSN string) bool {
    if (!IsValidURL(testDSN)) {
        return false
    }
    // It is safe to use this loose DSN pattern as we already confirmed that this is a valid URL
    matched, err := regexp.MatchString(`^https?:\/\/.+@.+\/[0-9]+$`, testDSN)
    if err != nil {
		fmt.Println("IsValidDSN::Error compiling regex:", err)
	}
    if !matched {
        fmt.Println("Invalid DSN: " + testDSN)
    }
    return matched
}

func generateSentryURLParams(authHeaderOrRequestURL string) string {
    if (authHeaderOrRequestURL == "") {
        return ""
    }

    var sentryKey string = ""
    var sentryVersion string = ""
    var sentryClient string = ""
    var extractedParam []string

    reKey, err := regexp.Compile(`sentry_key=([\w]+)`)
    if err != nil {
        fmt.Println("generateSentryURLParams::Error compiling regex: ", err)
    } else {
        extractedParam = reKey.FindStringSubmatch(authHeaderOrRequestURL)
        if len(extractedParam) > 1 {
            sentryKey = extractedParam[1]
        }
    }

    reVersion, err := regexp.Compile(`sentry_version=([\w]+)`)
    if err != nil {
        fmt.Println("generateSentryURLParams::Error compiling regex: ", err)
    } else {
        extractedParam = reVersion.FindStringSubmatch(authHeaderOrRequestURL)
        if len(extractedParam) > 1 {
            sentryVersion = extractedParam[1]
        }
    }

    reClient, err := regexp.Compile(`sentry_client=([\w/.]+)`)
    if err != nil {
        fmt.Println("generateSentryURLParams::Error compiling regex: ", err)
    } else {
        extractedParam = reClient.FindStringSubmatch(authHeaderOrRequestURL)
        if len(extractedParam) > 1 {
            sentryClient = extractedParam[1]
        }
    }

    return "sentry_version=" + sentryVersion + "&sentry_client=" + sentryClient + "&sentry_secret=" + sentryKey     
}

func constructSentryURL(dsn string, authHeaderOrRequestURL string) string {
    fmt.Println("constructSentryURL::Received DSN: ", dsn)
    
    var url string = ""
    var publicKey string = ""
    var hostPathProject = ""
    var hostPath string = ""
    var projectId string = ""
    var endPoint string = "envelope"
    
    parts := strings.Split(dsn, "//")
    if (len(parts) > 1) {
        url = parts[1]
    } else {
        url = dsn
    }
    parts = strings.Split(url, "@")
    if len(parts) < 2 {
        fmt.Println("constructSentryURL::Invalid dsn, missing @")
        return ""
    }
    publicKey = parts[0]
    hostPathProject = parts[1]
    parts = strings.Split(hostPathProject, "/")
    if len(parts) < 2 {
        fmt.Println("constructSentryURL::Invalid dsn, projectId is missing")
        return ""
    }
    hostPath = parts[0]
    projectId = parts[1]

    baseURI := "https://" + hostPath

    sentryURL := baseURI + "/api/" + projectId + "/" + endPoint + "/?sentry_key=" + publicKey
    
    authQueryParams := generateSentryURLParams(authHeaderOrRequestURL)
    sentryURL = sentryURL + "&" + authQueryParams

    return sentryURL
}

func getSentryAuth(header map[string][]string) string {
    if len(header["X-Sentry-Auth"]) == 0 {
        return ""
    }
    if len(header["X-Sentry-Auth"][0]) == 0 {
        return ""
    }
    return header["X-Sentry-Auth"][0]
}

// removeHeaders modifies the request to remove specific headers
func ModifyRequestHeaders(header map[string][]string) {
    // List of headers to remove
    headersToRemove := []string{"X-Sentry-Auth"}

    for _, key := range headersToRemove {
        delete(header, key)
    }
}

// Taking the received request object, creating a new request from it and sending it to the target url
func ForwardRequest(w http.ResponseWriter, target string, body []byte, headers map[string][]string) {
	// TODO: In order to support Sessions and Session Replay, these datamodels will need to be extracted from the envelope 
    //        and be sent separately to the default project in a new envelope

	// Create a new request based on the original request with the modified headers
	newReq, err := http.NewRequest("POST", target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Failed to create new request", http.StatusInternalServerError)
		return
	}
	newReq.Header = headers

	// Send the modified request to the target server
	client := &http.Client{}
	resp, err := client.Do(newReq)
	if err != nil {
		http.Error(w, "Failed to forward request", http.StatusInternalServerError)
        // TODO: If it was sent for a specific component and failed, retry to the default DSN - Nice to have
		return
	}
	defer resp.Body.Close()
}

// Worker function to process requests
func worker(id int, tasks <-chan RequestTask) {

    for task := range tasks {

        var jsonBody string = ""
        var componentName string = ""
        var targetURL string = ""
        // Cloning the defaultDSN into a local variable INSIDE THIS SCOPE
        dsn := defaultDSN

        // Checking if the body is encrypted
        reader := bytes.NewReader(task.Body)
        //Create a gzip reader to decompress the data
        gzipReader, err := gzip.NewReader(reader)
        if err != nil {
            jsonBody = string(task.Body)
        } else {
            // Read the decompressed data
            decompressedData, err := io.ReadAll(gzipReader)
            if err != nil {
                fmt.Println("Error reading decompressed data:", err)
            }
            jsonBody = string(decompressedData)
        }
        defer gzipReader.Close()

        jsonStrings := strings.Split(jsonBody, "\n")

        var event Event
        
        for _, value := range jsonStrings {
            err = json.Unmarshal([]byte(value), &event)
            if err == nil {
                if event.Tags != nil {
                    if _, exists := event.Tags[componentTagName]; exists {
                        componentName = event.Tags[componentTagName]
                        break
                    }
                }
            }   
        }

        if componentName != "" { // Not allowing "" as a valid component name
            if (len(ComponentToDSNMapping[componentName]) > 0 && IsValidDSN(ComponentToDSNMapping[componentName])) {
                dsn = ComponentToDSNMapping[componentName]
            } else {
                fmt.Println("the DSN for component " + componentName + " is missing or invalid")
            }
        }

        // getting the Sentry Auth Header
        sentryAuth := getSentryAuth(task.Header)

        // constructing a new request url based on DSN and sentry auth data
        if sentryAuth == "" {
            targetURL = constructSentryURL(dsn, task.URL)
        } else {
            targetURL = constructSentryURL(dsn, sentryAuth)
        }

        if !IsValidURL(targetURL) {
            fmt.Println("targetURL is invalid, dropping request %s %s %s\n", task.Method, task.URL, jsonBody)
            continue
        }
        
        // Removeing Sentry auth header from the request
        ModifyRequestHeaders(task.Header)
        
        // Forwarding the request to the right project
        ForwardRequest(task.Writer, targetURL, task.Body, task.Header)
    }
}

func callFunctionEvery(interval time.Duration, function func()) {
    ticker := time.NewTicker(interval)
    go func() {
        for {
            select {
            case <-ticker.C:
                function()
            }
        }
    }()
}

func loadConfigFile(configFileName string) bool {
    configFile, err := os.Open(configFileName)
    if err != nil {
        fmt.Println(err)
        return false
    }
    defer configFile.Close()

    // Read the file content
    byteValue, err := io.ReadAll(configFile)
    if err != nil {
        fmt.Println("Error reading config file content: ", err)
        return false
    }
    
    // Unmarshall the JSON data into struct
    var config Config
    err = json.Unmarshal(byteValue, &config)
    if err != nil {
        fmt.Println("Config file JSON format is corrupt: ", err)
        return false
    }

    ComponentToDSNMapping = config.Mapping
    return true
}

func periodicFunction() {
    loadConfigFile(configFilePath)
}

func isNumWorkersValid(arg string) bool {
    // Attempt to convert the string to an integer
    num, err := strconv.Atoi(arg)
    if err != nil {
        fmt.Println("Conversion failed, numWorkers not a valid integer")
        return false
    }
    if num < 1 || num > 100 {
        fmt.Println("numWorkers must be a positive integer no greater than 100")
        return false
    }
    numWorkers = num
    return true
} 

func main() {
    if len(os.Args) < 4 {
        fmt.Println("Missing arguments. <defaultDSN>, <configFilePath> and <numberOfGoWorkers> must be provided")
        os.Exit(1)
    }
    if !IsValidDSN(os.Args[1]) { // Or unreachable -> This is a MUST CHECK!!! , We will need to make a fake request
        fmt.Println("Invalid defaultDSN")
        os.Exit(1)
    }
    if !loadConfigFile(os.Args[2]) {
        fmt.Println("Error loading config file")
        os.Exit(1)
    }
    if !isNumWorkersValid(os.Args[3]) {
        fmt.Println("Invalid value/number of go workers")
        os.Exit(1)
    }
    fmt.Println("Initial config file was loaded")
    defaultDSN = os.Args[1]
    configFilePath = os.Args[2]

    //Start worker goroutines
    fmt.Println("Number of go workers: ",numWorkers )
    for i := 0; i < numWorkers; i++ {
        go worker(i, requestChan)
    }

    // Register handler function for the root URL pattern "/"
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
         // Read request body
        body, err := io.ReadAll(r.Body)
        if err != nil {
            fmt.Println("Failed to read request body", http.StatusInternalServerError)
        }
        
        task := RequestTask{
            Method: r.Method,
            URL:    r.URL.String(),
            Body:   body,
            Host:   r.Host,
            RequestURI: r.RequestURI,
            Header: r.Header,
            Writer: w,
        }
        requestChan <- task
    })

    // Call periodicFunction every 1 minute without blocking.
    callFunctionEvery(2*time.Minute, periodicFunction)

    // Start the HTTP server on "$Port"
    fmt.Println("Server listening on port " + Port)
    if err := http.ListenAndServe(Port, nil); err != nil {
        fmt.Printf("Failed to start server: %s\n", err)
    }
}