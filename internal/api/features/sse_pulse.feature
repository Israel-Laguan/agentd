Feature: SSE Live Pulse Broadcasting
  Scenario: Streaming events to a connected client
    Given an HTTP client connects to the SSE stream
    When the internal Sandbox emits a LOG_CHUNK event via the EventBus
    Then the HTTP client should receive the LOG_CHUNK event
    And the HTTP connection should not be closed by the server

  Scenario: Safe cleanup on client disconnect
    Given an HTTP client connects to the SSE stream
    When the HTTP client cancels the request
    Then the server active connection count should decrease by 1
