# Guest Agent Command Monitor
## Overview
The Guest Agent command monitor is a system used for executing commands in the guest agent on behalf of components in the guest os.

The events layer is formed of a **Monitor**, a **Server** and a **Handler** where the **Monitor** handles command registration for guest agent components, the **Server** is the component which listens for events from the gueest os, and the **Handler** is the function executed by the agent.

Each **Handler** is identified by a string ID, provided when sending commands to the server. Requests and response to and from the server are structured in JSON format. A request must contain the name field, specifying the handler to be executed. A request may contain arbitrary other fields to be passed to the handler. An example request is below:

```
{"Name":"agent.ExampleCommand","ArbitraryArgument":123}
```

A response will be valid JSON and has two required fields: Status and StatusMessage. Status is an int which follows unix status code conventions (ie zero is success, status codes are arbitrary and meaning is defined by the function called) and StatusMessage is an explanatory string accompanying the Status. Two example responses are below.

```
{"Status":0,"StatusMessage":""}

{"Status":7,"StatusMessage":"Failure message"}
```

By default, the Server listens on a unix socket or a named pipe, depending on platform. Permissions for the pipe and the pipe path can be set in the guest-agent [configuration](https://github.com/GoogleCloudPlatform/guest-agent#configuration). The default pipe path for windows and linux systems are `\\.\pipe\google-guest-agent-commands` non-windows and `/run/google-guest-agent/commands.sock` respectively.

## Implementing a command handler
Registering a command handler will expose the handler function to be called by anyone with write permission to the underlying socket. To do so, call `command.Get().RegisterHandler(name, handerFunc)` to get the current command monitor and register the handlerFunc with it. Note that if the command system is disabled by user configuration, handler registration will succeed but the server will not be available for callers to send commands to.
