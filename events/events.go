package events

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	dnstap "github.com/dnstap/golang-dnstap"
	"github.com/miekg/dns"
)

// DNSEvent represents a processed DNS event
type DNSEvent struct {
	Timestamp       string   `json:"timestamp"`
	SourceIP        string   `json:"source_ip,omitempty"`
	QueryName       string   `json:"query_name,omitempty"`
	QueryType       string   `json:"query_type,omitempty"`
	ResponseCode    string   `json:"response_code,omitempty"`
	ResponseData    []string `json:"response_data,omitempty"`
	MessageType     string   `json:"message_type"`
	Transport       string   `json:"transport,omitempty"`
	QueryTime       string   `json:"query_time,omitempty"`
	ResponseTime    string   `json:"response_time,omitempty"`
	ServerIP        string   `json:"server_ip,omitempty"`
	QueryPort       int      `json:"query_port,omitempty"`
	ResponsePort    int      `json:"response_port,omitempty"`
	MessageSize     int      `json:"message_size,omitempty"`
	QueryMessage    []byte   `json:"query_message,omitempty"`
	ResponseMessage []byte   `json:"response_message,omitempty"`
	// Splunk metadata fields
	SplunkSourcetype string `json:"sourcetype,omitempty"`
	SplunkHost       string `json:"host,omitempty"`
	SplunkIndex      string `json:"index,omitempty"`
	SplunkSource     string `json:"source,omitempty"`
}

// ToJSON converts an event to JSON string
func (e *DNSEvent) ToJSON() string {
	jsonBytes, err := json.Marshal(e)
	if err != nil {
		log.Printf("Error marshaling event to JSON: %v", err)
		return "{\"error\":\"json_marshal_failed\"}"
	}
	return string(jsonBytes)
}

// Processor handles DNSTAP message processing
type Processor struct {
	SplunkSourcetype string
	SplunkHost       string
	SplunkIndex      string
	SplunkSource     string
}

// NewProcessor creates a new DNSTAP processor
func NewProcessor() *Processor {
	return &Processor{}
}

// NewProcessorWithSplunkMetadata creates a new DNSTAP processor with Splunk metadata
func NewProcessorWithSplunkMetadata(sourcetype, host, index, source string) *Processor {
	return &Processor{
		SplunkSourcetype: sourcetype,
		SplunkHost:       host,
		SplunkIndex:      index,
		SplunkSource:     source,
	}
}

// ProcessMessage processes a single DNSTAP message and returns a DNSEvent
func (p *Processor) ProcessMessage(dt *dnstap.Dnstap) (*DNSEvent, error) {
	if dt.Message == nil {
		return nil, nil
	}

	msg := dt.Message
	event := &DNSEvent{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		MessageType: msg.GetType().String(),
	}

	// Set Splunk metadata fields
	if p.SplunkSourcetype != "" {
		event.SplunkSourcetype = p.SplunkSourcetype
	}
	if p.SplunkHost != "" {
		event.SplunkHost = p.SplunkHost
	}
	if p.SplunkIndex != "" {
		event.SplunkIndex = p.SplunkIndex
	}
	if p.SplunkSource != "" {
		event.SplunkSource = p.SplunkSource
	}

	// Extract IP addresses and ports
	if msg.QueryAddress != nil {
		event.SourceIP = net.IP(msg.QueryAddress).String()
	}
	if msg.ResponseAddress != nil {
		event.ServerIP = net.IP(msg.ResponseAddress).String()
	}
	if msg.QueryPort != nil {
		event.QueryPort = int(*msg.QueryPort)
	}
	if msg.ResponsePort != nil {
		event.ResponsePort = int(*msg.ResponsePort)
	}

	// Extract transport protocol
	if msg.SocketProtocol != nil {
		switch *msg.SocketProtocol {
		case dnstap.SocketProtocol_UDP:
			event.Transport = "UDP"
		case dnstap.SocketProtocol_TCP:
			event.Transport = "TCP"
		}
	}

	// Extract timestamps
	if msg.QueryTimeSec != nil && msg.QueryTimeNsec != nil {
		queryTime := time.Unix(int64(*msg.QueryTimeSec), int64(*msg.QueryTimeNsec))
		event.QueryTime = queryTime.UTC().Format(time.RFC3339Nano)
	}
	if msg.ResponseTimeSec != nil && msg.ResponseTimeNsec != nil {
		responseTime := time.Unix(int64(*msg.ResponseTimeSec), int64(*msg.ResponseTimeNsec))
		event.ResponseTime = responseTime.UTC().Format(time.RFC3339Nano)
	}

	// Parse DNS query message
	if msg.QueryMessage != nil {
		event.QueryMessage = msg.QueryMessage
		event.MessageSize = len(msg.QueryMessage)
		if dnsMsg, err := p.parseDNSMessage(msg.QueryMessage); err == nil {
			if len(dnsMsg.Question) > 0 {
				event.QueryName = dnsMsg.Question[0].Name
				event.QueryType = dns.TypeToString[dnsMsg.Question[0].Qtype]
			}
		}
	}

	// Parse DNS response message
	if msg.ResponseMessage != nil {
		event.ResponseMessage = msg.ResponseMessage
		if len(msg.QueryMessage) == 0 { // Only set size if not already set from query
			event.MessageSize = len(msg.ResponseMessage)
		}
		if dnsMsg, err := p.parseDNSMessage(msg.ResponseMessage); err == nil {
			event.ResponseCode = dns.RcodeToString[dnsMsg.Rcode]

			// Extract answer records (limit to prevent huge payloads)
			var answers []string
			for i, rr := range dnsMsg.Answer {
				if i >= 10 { // Limit to first 10 answers
					answers = append(answers, fmt.Sprintf("... and %d more records", len(dnsMsg.Answer)-10))
					break
				}
				answers = append(answers, rr.String())
			}
			if len(answers) > 0 {
				event.ResponseData = answers
			}
		}
	}

	return event, nil
}

// parseDNSMessage parses a DNS wire format message
func (p *Processor) parseDNSMessage(wireMsg []byte) (*dns.Msg, error) {
	msg := new(dns.Msg)
	err := msg.Unpack(wireMsg)
	return msg, err
}
