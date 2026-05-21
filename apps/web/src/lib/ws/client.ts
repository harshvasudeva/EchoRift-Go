export type RealtimeEvent<TPayload = unknown> = {
  id: string;
  type: string;
  workspace_id?: string;
  channel_id?: string;
  room_id?: string;
  payload?: TPayload;
  created_at?: string;
};

export type ClientRealtimeEvent<TPayload = unknown> = {
  id?: string;
  type: string;
  workspace_id?: string;
  channel_id?: string;
  room_id?: string;
  payload?: TPayload;
};

export function createRealtimeSocket(accessToken: string): WebSocket {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return new WebSocket(
    `${protocol}//${window.location.host}/api/ws?access_token=${encodeURIComponent(accessToken)}`,
  );
}

export function sendRealtimeEvent(
  socket: WebSocket,
  event: ClientRealtimeEvent,
): void {
  if (socket.readyState !== WebSocket.OPEN) {
    return;
  }
  socket.send(JSON.stringify(event));
}
