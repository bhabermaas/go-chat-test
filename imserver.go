// This is a simple chat server. It accepts connections, receive messages from clients and
// then echos the messages to all clients
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
)

// Packet : nput/Output packet structure for client(s)
type Packet struct {
	Action string
	Userid string
	Data   string
}

// Message : Output channel structure for a message
// Used when sending a message to the broadcaster
type Message struct {
	Userid string
	Data   string
	Clinst Instance
}

type client chan<- Message

// Instance structure for a chat client. Just a nice place to
// keep goodies for every client.
type Instance struct {
	Userid  string
	Channel client
	Connect net.Conn
	RW      *bufio.ReadWriter
}

// Common channels and client list
var (
	broadcast = make(chan Message, 10)
	entering  = make(chan Instance, 10)
	leaving   = make(chan Instance, 10)
	clients   = make(map[string]Instance)
)

// StartServer : start server and accept connections. At each accepted connection a goroutine is
// started to handle input from its client.
//
func StartServer() {

	log.Print("Chat server started")
	listener, err := net.Listen("tcp", "localhost:8000")
	if err != nil {
		log.Fatal(err)
	}

	// Start broadcaster background goroutine
	go broadcaster()

	// Loop around listening and accepting client connections.
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		// talk to the client from this goroutine
		go handleConn(conn)
	}
}

// Handle a client connection. Receive JSON packets, handle actions, dispatch
// messages to broadcaster
//
func handleConn(conn net.Conn) {

	var userid = "unknown"

	log.Print("Handling connection from ", conn.RemoteAddr().String())

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// Initialize the client instance and message channel
	instance := Instance{}
	instance.Connect = conn
	instance.RW = rw
	ch := make(chan Message, 10)
	instance.Channel = ch

	for {
		// Read the next packet from this client
		response, err := rw.ReadString('\n')
		if err != nil {
			log.Print(err)
			if _, err := rw.Peek(1); err == io.EOF {
				log.Printf("User %s has unexpectedly disconnected %s", userid, err)
				conn.Close()
				return
			}
			continue
		}

		packet := Packet{}
		err = json.Unmarshal([]byte(response), &packet)
		if err != nil {
			log.Printf("Unable to unmarshal package for %s, err=%s\n", userid, err)
			log.Fatal("Server is stopping\n")
		}

		msg := Message{}
		msg.Clinst = instance

		// The first packet from user is LOGIN so we know the identity of the
		// user when sending messages

		if packet.Action == "LOGIN" {

			userid = packet.Userid
			instance.Userid = userid

			msg.Userid = userid

			// Start client writer goroutine
			go clientWriter(rw, ch, userid)

			// Check if userid already registered
			if _, ok := clients[userid]; ok {
				msg.Data = fmt.Sprintf("userid %s is already logged in", userid)
				broadcast <- msg
				break
			}

			entering <- instance
			msg.Data = fmt.Sprintf("Entered chat (%s)", conn.RemoteAddr().String())
			broadcast <- msg
			continue
		}

		// Make sure we remember who this is
		msg.Userid = userid

		// When the client terminates trigger cleanup
		if packet.Action == "QUIT" {
			msg.Data = fmt.Sprintf("Left chat")
			broadcast <- msg
			leaving <- instance
			return
		}

		// Must be a message so send it out on the broadcast channel
		msg.Data = packet.Data
		broadcast <- msg
	}

	// That's all folks!
	conn.Close()
	close(instance.Channel)
}

// goroutine to broadcast messages to all chat clients. It also monitors entering and
// leaving channels to maintain the client list
//
func broadcaster() {
	log.Print("broadcaster running...")
	for {
		select {
		case msg := <-broadcast:

			// broadcast to all clients. This goes out on the channel connected to the
			// clientWriter
			for _, instance := range clients {
				instance.Channel <- msg
			}

		// Somebody arrived
		case instance := <-entering:
			clients[instance.Userid] = instance

		// Somebody has left
		case instance := <-leaving:
			delete(clients, instance.Userid)
			close(instance.Channel)
		}
	}
}

// goroutine to write a message to a specific client. There is one routine per client.
// This takes messages from the channel and writes them to the client.
//
func clientWriter(rw *bufio.ReadWriter, ch <-chan Message, userid string) {
	log.Printf("clientWriter running.(%s) ...", userid)

	for msg := range ch {

		if userid == msg.Userid {
			// Don't echo a message back to it's originator
			continue
		}

		// Build the message packet and send
		packet := Packet{}
		packet.Userid = msg.Userid
		packet.Data = msg.Data
		packet.Action = "MSG"
		writePacketToClient(rw, packet)
	}
	// When the channel is closed, the 'for' loop ends.
	log.Printf("clientWriter leaving (%s)", userid)
}

//
// Write a message packet to chat client. The message is converted into JSON and
// then sent to the client.
//
func writePacketToClient(rw *bufio.ReadWriter, packet Packet) {

	stream, err := json.Marshal(packet)
	if err != nil {
		log.Print("writePacketToClient marshal failed ", err)
		return
	}
	s := fmt.Sprintf("%s\n", stream)
	_, err = rw.WriteString(s)
	rw.Flush()
	if err != nil {
		log.Fatal("writePacketToClient write failed ", err)
	}
}
