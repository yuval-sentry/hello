# Sentry Relay Proxy

This repository contains a Sentry Relay Proxy written in Go. The proxy forwards requests to Sentry, dynamically routing them based on the specified component tags.

## Table of Contents

- [Introduction](#introduction)
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Contributing](#contributing)
- [License](#license)
- [Contact](#contact)

## Introduction

The Sentry Relay Proxy is designed to forward incoming HTTP requests to different Sentry projects based on a configuration file. This enables dynamic routing and load balancing of Sentry events.

## Features

- Validate and forward Sentry requests.
- Dynamic DSN mapping based on component tags.
- Periodic configuration reload.
- Support for compressed request bodies.

## Prerequisites

Ensure you have the following installed:

- [Go](https://golang.org/doc/install) (version 1.16 or later)
- [Git](https://git-scm.com/)

## Installation

1. **Clone the repository:**

    ```sh
    git clone https://github.com/yuval-sentry/hello.git
    ```

2. **Navigate to the project directory:**

    ```sh
    cd hello
    ```

3. **Build the project:**

    ```sh
    go build -o sentry-relay-proxy
    ```

## Configuration

Create a configuration file in JSON format to map components to their respective DSNs. For example, `config.json`:

```json
{
    "mapping": {
        "componentA": "https://examplePublicKey@o0.ingest.sentry.io/0",
        "componentB": "https://examplePublicKey@o0.ingest.sentry.io/1"
    }
}

## Usage
Run the proxy with the following command:
./sentry-relay-proxy <defaultDSN> <configFilePath> <numberOfGoWorkers>

Example
./sentry-relay-proxy https://defaultPublicKey@o0.ingest.sentry.io/0 ./config.json 15

This starts the server on port 8080. The server listens for incoming requests and forwards them based on the component tags defined in the configuration file.
