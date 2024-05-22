package main

import (
	"fmt"
    "io"
	"io/ioutil"
	"net/http"
    "bytes"
    "regexp"
    "compress/gzip"
    "strings"
    "encoding/json"
    "os"
    "net/url"
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

const configFileName = "config.json" 

type Config struct {
    Mapping map[string]string `json:"mapping"`
}

var ComponentToDSNMapping map[string]string

// Channel to forward request details
var requestChan = make(chan RequestTask)

// Number of worker goroutines
const numWorkers = 15

const componentTagName = "sentry_relay_component"
// other options for this pattern : sentry_relay_component"\s?:\s?"([^"]+) or sentry_relay_component"\s*:\s*"([^"]+)
const componentNamePattern = `"sentry_relay_component":"([^"]+)"`

// func terminateProcess(exitCode int, message string) {
//     fmt.Println(message)
//     os.Exit(exitCode)
// }

// IsValidURL checks if a given URL string is valid.
func IsValidURL(testURL string) bool {
    _, err := url.ParseRequestURI(testURL)
    if err != nil {
        fmt.Println("IsValidURL :: " + testURL + " is invalid url ", err)
    }
    return err == nil
}

// func IsValidDSN(testDSN string) bool {
//     if (!IsValidURL(testDSN)) {
//         return false
//     }
//     // ^https?:\/\/.+@.+\.ingest(\.us)?\.sentry\.io\/[0-9]+$
//     // ^https://[a-zA-Z0-9]{32}@o[0-9]+\.ingest\.us\.sentry\.io/[0-9]+$
//     // ^https?:\/\/.+@.+\.ingest(\.us)?\.sentry\.io\/[0-9]+$

// }

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
    var endPoint string = "envelope" // TODO: Extract endpoint from URL
    
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

    fmt.Println("constructSentryURL:: return result sentryURL : ", sentryURL)

    return sentryURL
}

func getSentryAuth(header map[string][]string) string {
    if len(header["X-Sentry-Auth"]) == 0 {
        return ""
    }
    if len(header["X-Sentry-Auth"][0]) == 0 {
        return ""
    }
    fmt.Println("header[x]", header["X-Sentry-Auth"][0])
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
	
	// Create a new request based on the original request with the modified headers
	newReq, err := http.NewRequest("POST", target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Failed to create new request", http.StatusInternalServerError)
		return
	}
	newReq.Header = headers

    fmt.Printf("Entire new request object:   ")
    fmt.Printf("%#v\n", newReq)
    fmt.Printf("\n\n")

	// Send the modified request to the target server
	client := &http.Client{}
	resp, err := client.Do(newReq)
	if err != nil {
		http.Error(w, "Failed to forward request", http.StatusInternalServerError)
        // TODO: If it was sent for a specific component and failed, retry to the default DSN
		return
	}
    fmt.Println("Forwarded request response", resp)
	defer resp.Body.Close()
}

// Worker function to process requests
func worker(id int, tasks <-chan RequestTask) {
    var json string = ""
    var componentName string = ""
    // TODO: Check that the default DSN is valid and working
    var defaultDSN string = "https://efe273e1f9aae6f6f0bc4fb089fab1d7@o87286.ingest.us.sentry.io/4507262272208896"
    var dsn string = defaultDSN
    var targetURL string = ""
    
    for task := range tasks {
        fmt.Printf("\n\nReceived request: %s %s\n", task.Method, task.URL)

        for key, values := range task.Header {
            for _, value := range values {
                fmt.Printf("\t%s: %s\n", key, value)
            }
        }

        // Checking if the body is encrypted
        reader := bytes.NewReader(task.Body)
        //Create a gzip reader to decompress the data
        gzipReader, err := gzip.NewReader(reader)
        if err != nil {
            fmt.Printf("Body received raw, not gzipped")
            fmt.Printf("Body: %s\n", string(task.Body))
            json = string(task.Body)
        } else {
            // Read the decompressed data
            decompressedData, err := ioutil.ReadAll(gzipReader)
            if err != nil {
                fmt.Println("Error reading decompressed data:", err)
            }
            fmt.Println("Decompressed data:", string(decompressedData))
            json = string(decompressedData)
        }
        defer gzipReader.Close()

        // TODO: In case that there is a DSN specified inside the body we need to remove that and later on update the new requesrt body 
        // >>> with the new JSON

        // Extracting the component name from tag `sentry_relay_component`
        re, err := regexp.Compile(componentNamePattern)
        if err != nil {
            fmt.Println("Couldn't compile component name regex matcher " + componentNamePattern + " ", err)
        } else {
            matches := re.FindStringSubmatch(json)
            if matches != nil && len(matches) > 1 {
                componentName = matches[1]
                fmt.Println("Extracted value:", componentName)
                if (len(ComponentToDSNMapping[componentName]) > 0 && IsValidURL(ComponentToDSNMapping[componentName])) {
                    dsn = ComponentToDSNMapping[componentName]
                } else {
                    fmt.Println("the DSN for component " + componentName + " is missing or invalid")
                }
            } else {
                fmt.Println(componentTagName + " tag doesn't exists")
            }
        }
        fmt.Println("Past getting the component name")

        // getting the Sentry Auth Header
        sentryAuth := getSentryAuth(task.Header)
        fmt.Println("Past getSentryAuth")

        // constructing a new request url based on DSN and sentry auth data
        if sentryAuth == "" {
            targetURL = constructSentryURL(dsn, task.URL)
        } else {
            targetURL = constructSentryURL(dsn, sentryAuth)
        }
        fmt.Println("Past constructSentryURL")

        if !IsValidURL(targetURL) {
            fmt.Println("targetURL is invalid, dropping request %s %s %s\n", task.Method, task.URL, json)
            continue
        }
        
        // Removeing Sentry auth header from the request
        ModifyRequestHeaders(task.Header)
        fmt.Println("Past ModifyRequestHeaders")
        
        // Forwarding the request to the right project
        ForwardRequest(task.Writer, targetURL, task.Body, task.Header)
        fmt.Println("Past ForwardRequest")

        fmt.Printf("\n\n\n\n\n\n\n\n")
    }
}

func loadConfigFile(configFileName string) {
    configFile, err := os.Open(configFileName)
    if err != nil {
        fmt.Println(err)
        return // terminate process?
    }
    defer configFile.Close()

    // Read the file content
    byteValue, err := ioutil.ReadAll(configFile)
    if err != nil {
        fmt.Println("Error reading config file content: ", err)
        return // terminate process?
    }
    
    // Unmarshall the JSON data into struct
    var config Config
    err = json.Unmarshal(byteValue, &config)
    if err != nil {
        fmt.Println("Config file JSON format is corrupt: ", err)
        return // terminate process?
    }

    ComponentToDSNMapping = config.Mapping
}

func main() {
    // TODO: Get default DSN as a required param and validate that it's valid, if not, display an error message and kill the process
    // Loading config file
    loadConfigFile(configFileName)
    
    //Start worker goroutines
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

    // Start the HTTP server on "$Port"
    fmt.Println("Server listening on port " + Port)
    if err := http.ListenAndServe(Port, nil); err != nil {
        fmt.Printf("Failed to start server: %s\n", err)
    }
}