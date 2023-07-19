# Guest Agent Events Handling Layer
## Overview
The Guest Agent events handling layer is a generic and multi-purpose events handling layer, it's designed to offer a unified mechanism and API to manage and communicate "external" events across the guest agent implementation. Such "external" events could be a underlying Operating System event such as file changes or removal, sockets, named pipes etc as well as platform services such as metadata service, snapshot service etc.

The events layer is formed of a **Manager**, a **Watcher** and a **Subscriber** where the **Manager** is the events controller/manager itself, the **Watcher** is the implementation of the event listening and the **Subscriber** is the callback function interested in a given event and registered to handle it or "to be notified when they happen".

Each **Event** is internally identified by a string ID, when registering the **Subscriber** must tell what event it's interested on, such as:

```golang
  eventManager.Subscribe("metadata-watcher,longpoll", &userData, func(evType string, data interface{}, evData interface{}) bool {
	// Event handling implementation...
    return true
  })
```

The **Subscriber** implementation must return a boolean, such a boolean determines if the **Subscriber** must be renewed or if it must be unregistered/unsubscribed.

## Sequence Diagram
Below is a high level sequence diagram showing how the **Guest Agent**, **Manager**, **Watchers** and **Handlers/Subscribers** interact with each other:

```
┌───────────┐    ┌─────────────┐  ┌─────────┐┌─────────┐┌─────────┐┌─────────┐
│Guest Agent│    │Event Manager│  │Watcher A││Watcher B││Handler A││Handler B│
└─────┬─────┘    └──────┬──────┘  └────┬────┘└────┬────┘└────┬────┘└────┬────┘
      │                 │              │          │          │          │
      │   Initialize    │              │          │          │          │
      │────────────────>│              │          │          │          │
      │                 │              │          │          │          │
      │                 │   Register   │          │          │          │
      │                 │─────────────>│          │          │          │
      │                 │              │          │          │          │
      │                 │        Register         │          │          │
      │                 │────────────────────────>│          │          │
      │                 │              │          │          │          │
      │Done initializing│              │          │          │          │
      │<────────────────│              │          │          │          │
      │                 │              │          │          │          │
      │                 │   Subscribe()│          │          │          │
      │─────────────────────────────────────────────────────>│          │
      │                 │              │          │          │          │
      │                 │         Subscribe()     │          │          │
      │────────────────────────────────────────────────────────────────>│
      │                 │              │          │          │          │
      │      Run()      │              │          │          │          │
      │────────────────>│              │          │          │          │
      │                 │              │          │          │          │
      │                 │    Run()     │          │          │          │
      │                 │─────────────>│          │          │          │
      │                 │              │          │          │          │
      │                 │          Run()          │          │          │
      │                 │────────────────────────>│          │          │
      │                 │              │          │          │          │
      │                 │dispatch event│          │          │          │
      │                 │<─────────────│          │          │          │
      │                 │              │          │          │          │
      │                 │              │Call()    │          │          │
      │                 │───────────────────────────────────>│          │
      │                 │              │          │          │          │
      │                 │     dispatch event      │          │          │
      │                 │<────────────────────────│          │          │
      │                 │              │          │          │          │
      │                 │              │     Call()          │          │
      │                 │──────────────────────────────────────────────>│
┌─────┴─────┐    ┌──────┴──────┐  ┌────┴────┐┌────┴────┐┌────┴────┐┌────┴────┐
│Guest Agent│    │Event Manager│  │Watcher A││Watcher B││Handler A││Handler B│
└───────────┘    └─────────────┘  └─────────┘└─────────┘└─────────┘└─────────┘

```

## Built-in Watchers

|Watcher|Events|Desc|
|-------|------|----|
|metadata|metadata-watcher,longpoll|A new version of the metadata descriptor was detected.|
