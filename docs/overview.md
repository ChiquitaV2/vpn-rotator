# Overview

VPN Rotator is a self-hosted system that automatically rotates WireGuard VPN nodes on Hetzner Cloud to enhance privacy
and security. It provisions nodes on-demand, automatically destroys idle nodes to save costs, and handles seamless
client migration during rotation.

## Key Features

* **Automatic Node Rotation:** Periodically provisions new VPN nodes and decommissions old ones.
* **On-Demand Provisioning:** Automatically creates a new node if no active node is available.
* **Cost-Effective:** Destroys idle nodes to minimize infrastructure costs.
* **Seamless Client Migration:** The connector client automatically detects new nodes and reconnects.
* **Privacy-Focused:** No client connection logs are retained on the VPN nodes.

## System Components

* **Rotator Service:** The core backend service that manages the entire lifecycle of VPN nodes.
* **Connector Client:** A command-line tool for client devices to connect to the VPN and handle automatic rotation.
* **Database:** A local SQLite database for persisting the state of the system.
* **Hetzner Cloud:** The infrastructure provider for the WireGuard VPN nodes.
