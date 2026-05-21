import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { Room, RoomEvent, type RemoteParticipant } from "livekit-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { create } from "zustand";
import { APIError, apiGet, apiPost } from "./lib/api/client";
import {
  createRealtimeSocket,
  sendRealtimeEvent,
  type RealtimeEvent,
} from "./lib/ws/client";
import { useAuthStore, type User } from "./stores/auth";

let sessionRestorePromise: Promise<AuthResponse> | null = null;

function restoreSessionOnce(): Promise<AuthResponse> {
  if (!sessionRestorePromise) {
    sessionRestorePromise = apiPost<AuthResponse>(
      "/api/v1/auth/refresh",
    ).finally(() => {
      sessionRestorePromise = null;
    });
  }
  return sessionRestorePromise;
}

type UIState = {
  selectedWorkspaceID: string | null;
  selectedChannelID: string | null;
  setSelectedWorkspaceID: (workspaceID: string | null) => void;
  setSelectedChannelID: (channelID: string | null) => void;
};

const useUIStore = create<UIState>((set) => ({
  selectedWorkspaceID: null,
  selectedChannelID: null,
  setSelectedWorkspaceID: (workspaceID) =>
    set({ selectedWorkspaceID: workspaceID, selectedChannelID: null }),
  setSelectedChannelID: (channelID) => set({ selectedChannelID: channelID }),
}));

type PingResponse = {
  message: string;
};

type AuthResponse = {
  access_token: string;
  expires_in: number;
  token_type: "Bearer";
  user: User;
};

type Workspace = {
  id: string;
  name: string;
  slug: string;
  owner_user_id: string;
  created_at: string;
};

type Channel = {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  type: "text" | "voice" | "announcement";
  is_private: boolean;
  position: number;
  created_at: string;
};

type Message = {
  id: string;
  workspace_id: string;
  channel_id: string;
  author_user_id: string;
  author_display_name: string;
  body: string;
  created_at: string;
  edited_at: string | null;
};

type PresenceUser = {
  user_id: string;
  display_name: string;
  status: "online" | "offline";
};

type VoiceJoinResponse = {
  livekit_url: string;
  token: string;
  room_name: string;
  channel_id: string;
};

type VoiceParticipant = {
  identity: string;
  name: string;
  isLocal: boolean;
};

function getVoiceParticipants(
  room: Room,
  localDisplayName: string,
): VoiceParticipant[] {
  const localIdentity = room.localParticipant.identity;
  const localName =
    room.localParticipant.name || localDisplayName || localIdentity || "You";
  const remoteParticipants = Array.from(room.remoteParticipants.values()).map(
    (participant: RemoteParticipant) => ({
      identity: participant.identity,
      name: participant.name || participant.identity || "Participant",
      isLocal: false,
    }),
  );

  return [
    {
      identity: localIdentity || "local",
      name: localName,
      isLocal: true,
    },
    ...remoteParticipants,
  ];
}

export function App() {
  const accessToken = useAuthStore((state) => state.accessToken);
  const user = useAuthStore((state) => state.user);
  const setAuth = useAuthStore((state) => state.setAuth);
  const clearAuth = useAuthStore((state) => state.clearAuth);
  const [refreshChecked, setRefreshChecked] = useState(false);

  const ping = useQuery({
    queryKey: ["api", "ping"],
    queryFn: () => apiGet<PingResponse>("/api/v1/ping"),
  });

  useEffect(() => {
    let cancelled = false;
    restoreSessionOnce()
      .then((response) => {
        if (!cancelled) {
          setAuth(response.access_token, response.user);
        }
      })
      .catch(() => {
        if (!cancelled) {
          clearAuth();
        }
      })
      .finally(() => {
        if (!cancelled) {
          setRefreshChecked(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [clearAuth, setAuth]);

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100">
      <div className="mx-auto flex min-h-screen max-w-6xl flex-col px-6 py-8">
        <header className="mb-8 flex items-center justify-between border-b border-slate-800 pb-6">
          <div>
            <p className="text-sm uppercase tracking-[0.4em] text-cyan-400">
              EchoRift
            </p>
            <h1 className="mt-2 text-3xl font-bold">
              Realtime team communication
            </h1>
          </div>
          <div className="flex items-center gap-3">
            <div className="rounded-full border border-slate-700 px-4 py-2 text-sm text-slate-300">
              Backend:{" "}
              {ping.isLoading
                ? "checking"
                : ping.data?.message === "pong"
                  ? "online"
                  : "offline"}
            </div>
            {user ? (
              <button
                className="rounded-full bg-slate-800 px-4 py-2 text-sm text-slate-200 hover:bg-slate-700"
                onClick={async () => {
                  await apiPost("/api/v1/auth/logout").catch(() => undefined);
                  clearAuth();
                }}
              >
                Logout
              </button>
            ) : null}
          </div>
        </header>

        {!refreshChecked ? (
          <div className="rounded-2xl border border-slate-800 bg-slate-900/70 p-8 text-slate-300">
            Checking session…
          </div>
        ) : accessToken && user ? (
          <WorkspaceShell accessToken={accessToken} user={user} />
        ) : (
          <AuthPanel
            onAuth={(response) => setAuth(response.access_token, response.user)}
          />
        )}
      </div>
    </main>
  );
}

function AuthPanel({ onAuth }: { onAuth: (response: AuthResponse) => void }) {
  const [mode, setMode] = useState<"login" | "signup">("signup");
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const body =
        mode === "signup"
          ? {
              email,
              password,
              display_name: displayName,
              device_name: "Web Browser",
              platform: "web",
            }
          : { email, password, device_name: "Web Browser", platform: "web" };
      const response = await apiPost<AuthResponse>(
        mode === "signup" ? "/api/v1/auth/signup" : "/api/v1/auth/login",
        body,
      );
      onAuth(response);
    } catch (caught) {
      if (caught instanceof APIError) {
        setError(caught.code);
      } else {
        setError("network_error");
      }
    } finally {
      setLoading(false);
    }
  }

  return (
    <section className="mx-auto w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900/70 p-6 shadow-2xl shadow-cyan-950/20">
      <div className="mb-6">
        <h2 className="text-2xl font-bold">
          {mode === "signup" ? "Create your account" : "Welcome back"}
        </h2>
        <p className="mt-2 text-sm text-slate-400">
          Go-native auth with Argon2id passwords and refresh-token rotation.
        </p>
      </div>

      <form className="space-y-4" onSubmit={submit}>
        <label className="block text-sm font-medium text-slate-300">
          Email
          <input
            className="mt-1 w-full rounded-xl border border-slate-700 bg-slate-950 px-4 py-3 text-slate-100 outline-none ring-cyan-500/30 focus:ring-4"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            required
          />
        </label>

        {mode === "signup" ? (
          <label className="block text-sm font-medium text-slate-300">
            Display name
            <input
              className="mt-1 w-full rounded-xl border border-slate-700 bg-slate-950 px-4 py-3 text-slate-100 outline-none ring-cyan-500/30 focus:ring-4"
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              placeholder="Harsh"
            />
          </label>
        ) : null}

        <label className="block text-sm font-medium text-slate-300">
          Password
          <input
            className="mt-1 w-full rounded-xl border border-slate-700 bg-slate-950 px-4 py-3 text-slate-100 outline-none ring-cyan-500/30 focus:ring-4"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            minLength={8}
            required
          />
        </label>

        {error ? (
          <div className="rounded-xl border border-red-900 bg-red-950/40 p-3 text-sm text-red-200">
            {error}
          </div>
        ) : null}

        <button
          className="w-full rounded-xl bg-cyan-500 px-4 py-3 font-semibold text-slate-950 hover:bg-cyan-400 disabled:cursor-not-allowed disabled:opacity-60"
          type="submit"
          disabled={loading}
        >
          {loading ? "Working…" : mode === "signup" ? "Sign up" : "Log in"}
        </button>
      </form>

      <button
        className="mt-4 w-full text-sm text-cyan-300 hover:text-cyan-200"
        onClick={() => {
          setMode(mode === "signup" ? "login" : "signup");
          setError(null);
        }}
      >
        {mode === "signup"
          ? "Already have an account? Log in"
          : "Need an account? Sign up"}
      </button>
    </section>
  );
}

function WorkspaceShell({
  accessToken,
  user,
}: {
  accessToken: string;
  user: User;
}) {
  const queryClient = useQueryClient();
  const selectedWorkspaceID = useUIStore((state) => state.selectedWorkspaceID);
  const selectedChannelID = useUIStore((state) => state.selectedChannelID);
  const setSelectedWorkspaceID = useUIStore(
    (state) => state.setSelectedWorkspaceID,
  );
  const setSelectedChannelID = useUIStore(
    (state) => state.setSelectedChannelID,
  );
  const [workspaceName, setWorkspaceName] = useState("");
  const [messageBody, setMessageBody] = useState("");
  const [realtimeStatus, setRealtimeStatus] = useState<
    "connecting" | "connected" | "disconnected" | "error"
  >("disconnected");
  const [presence, setPresence] = useState<Record<string, PresenceUser>>({});
  const [typingUsers, setTypingUsers] = useState<Record<string, string>>({});
  const [voiceError, setVoiceError] = useState<string | null>(null);
  const [joinedVoiceChannelID, setJoinedVoiceChannelID] = useState<
    string | null
  >(null);
  const [voiceParticipants, setVoiceParticipants] = useState<
    VoiceParticipant[]
  >([]);
  const socketRef = useRef<WebSocket | null>(null);
  const typingStopTimer = useRef<number | undefined>(undefined);
  const liveKitRoomRef = useRef<Room | null>(null);

  const workspacesQuery = useQuery({
    queryKey: ["workspaces"],
    queryFn: () =>
      apiGet<{ workspaces: Workspace[] }>("/api/v1/workspaces", {
        accessToken,
      }),
  });

  const channelsQuery = useQuery({
    queryKey: ["channels", selectedWorkspaceID],
    queryFn: () =>
      apiGet<{ channels: Channel[] }>(
        `/api/v1/workspaces/${selectedWorkspaceID}/channels`,
        { accessToken },
      ),
    enabled: Boolean(selectedWorkspaceID),
  });

  const textChannels = useMemo(
    () =>
      channelsQuery.data?.channels.filter(
        (channel) => channel.type === "text" || channel.type === "announcement",
      ) ?? [],
    [channelsQuery.data],
  );

  const voiceChannels = useMemo(
    () =>
      channelsQuery.data?.channels.filter(
        (channel) => channel.type === "voice",
      ) ?? [],
    [channelsQuery.data],
  );

  const messagesQuery = useQuery({
    queryKey: ["messages", selectedWorkspaceID, selectedChannelID],
    queryFn: () =>
      apiGet<{ messages: Message[] }>(
        `/api/v1/workspaces/${selectedWorkspaceID}/channels/${selectedChannelID}/messages`,
        { accessToken },
      ),
    enabled: Boolean(selectedWorkspaceID && selectedChannelID),
  });

  const createWorkspace = useMutation({
    mutationFn: (name: string) =>
      apiPost<{ workspace: Workspace }>(
        "/api/v1/workspaces",
        { name },
        { accessToken },
      ),
    onSuccess: async (response) => {
      setWorkspaceName("");
      setSelectedWorkspaceID(response.workspace.id);
      await queryClient.invalidateQueries({ queryKey: ["workspaces"] });
    },
  });

  const sendMessage = useMutation({
    mutationFn: (body: string) =>
      apiPost<{ message: Message }>(
        `/api/v1/workspaces/${selectedWorkspaceID}/channels/${selectedChannelID}/messages`,
        { body },
        { accessToken },
      ),
    onSuccess: async () => {
      setMessageBody("");
      sendTypingStop();
      await queryClient.invalidateQueries({
        queryKey: ["messages", selectedWorkspaceID, selectedChannelID],
      });
    },
  });

  const joinVoice = useMutation({
    mutationFn: (channelID: string) =>
      apiPost<VoiceJoinResponse>(
        `/api/v1/workspaces/${selectedWorkspaceID}/voice/${channelID}/join`,
        {},
        { accessToken },
      ),
    onSuccess: async (response) => {
      setVoiceError(null);
      liveKitRoomRef.current?.disconnect();
      const room = new Room();
      liveKitRoomRef.current = room;

      const updateVoiceParticipants = () => {
        setVoiceParticipants(getVoiceParticipants(room, user.display_name));
      };

      room
        .on(RoomEvent.ParticipantConnected, updateVoiceParticipants)
        .on(RoomEvent.ParticipantDisconnected, updateVoiceParticipants)
        .on(RoomEvent.ParticipantNameChanged, updateVoiceParticipants)
        .on(RoomEvent.ParticipantMetadataChanged, updateVoiceParticipants);

      await room.connect(response.livekit_url, response.token);
      updateVoiceParticipants();
      await room.localParticipant.setMicrophoneEnabled(true);
      setJoinedVoiceChannelID(response.channel_id);
    },
    onError: (caught) => {
      if (caught instanceof APIError) {
        setVoiceError(caught.code);
      } else {
        setVoiceError("voice_join_failed");
      }
    },
  });

  function leaveVoice() {
    liveKitRoomRef.current?.disconnect();
    liveKitRoomRef.current = null;
    setJoinedVoiceChannelID(null);
    setVoiceParticipants([]);
  }

  function sendTypingStop() {
    if (selectedWorkspaceID && selectedChannelID && socketRef.current) {
      sendRealtimeEvent(socketRef.current, {
        type: "typing.stop",
        workspace_id: selectedWorkspaceID,
        channel_id: selectedChannelID,
      });
    }
  }

  function sendTypingStart() {
    if (!selectedWorkspaceID || !selectedChannelID || !socketRef.current) {
      return;
    }
    sendRealtimeEvent(socketRef.current, {
      type: "typing.start",
      workspace_id: selectedWorkspaceID,
      channel_id: selectedChannelID,
    });
    if (typingStopTimer.current !== undefined) {
      window.clearTimeout(typingStopTimer.current);
    }
    typingStopTimer.current = window.setTimeout(sendTypingStop, 1200);
  }

  useEffect(() => {
    return () => {
      leaveVoice();
    };
  }, []);

  useEffect(() => {
    const firstWorkspace = workspacesQuery.data?.workspaces[0];
    if (!selectedWorkspaceID && firstWorkspace) {
      setSelectedWorkspaceID(firstWorkspace.id);
    }
  }, [selectedWorkspaceID, setSelectedWorkspaceID, workspacesQuery.data]);

  useEffect(() => {
    const firstTextChannel = textChannels[0];
    if (!selectedChannelID && firstTextChannel) {
      setSelectedChannelID(firstTextChannel.id);
    }
  }, [selectedChannelID, setSelectedChannelID, textChannels]);

  const activeWorkspace = workspacesQuery.data?.workspaces.find(
    (workspace) => workspace.id === selectedWorkspaceID,
  );
  const activeChannel = textChannels.find(
    (channel) => channel.id === selectedChannelID,
  );
  const onlineUsers = Object.values(presence);
  const typingNames = Object.values(typingUsers);

  useEffect(() => {
    if (!accessToken || !selectedWorkspaceID) {
      setRealtimeStatus("disconnected");
      return;
    }

    let stopped = false;
    let reconnectTimer: number | undefined;
    let socket: WebSocket | undefined;

    function connect() {
      if (stopped) {
        return;
      }
      setRealtimeStatus("connecting");
      socket = createRealtimeSocket(accessToken);
      socketRef.current = socket;

      socket.onopen = () => {
        setRealtimeStatus("connected");
        if (socket && selectedWorkspaceID) {
          sendRealtimeEvent(socket, {
            type: "workspace.subscribe",
            workspace_id: selectedWorkspaceID,
          });
        }
      };

      socket.onmessage = (messageEvent) => {
        try {
          const event = JSON.parse(String(messageEvent.data)) as RealtimeEvent<{
            message?: Message;
            code?: string;
          }>;

          if (
            event.type === "message.created" &&
            event.workspace_id &&
            event.channel_id
          ) {
            void queryClient.invalidateQueries({
              queryKey: ["messages", event.workspace_id, event.channel_id],
            });
            return;
          }

          if (event.type === "workspace.subscribed") {
            const payload = event.payload as unknown as {
              presence?: PresenceUser[];
            };
            const next: Record<string, PresenceUser> = {};
            for (const item of payload.presence ?? []) {
              next[item.user_id] = item;
            }
            setPresence(next);
            return;
          }

          if (event.type === "presence.updated") {
            const payload = event.payload as unknown as PresenceUser;
            setPresence((current) => {
              const next = { ...current };
              if (payload.status === "offline") {
                delete next[payload.user_id];
              } else {
                next[payload.user_id] = payload;
              }
              return next;
            });
            return;
          }

          if (
            (event.type === "typing.start" || event.type === "typing.stop") &&
            event.channel_id === selectedChannelID
          ) {
            const payload = event.payload as unknown as {
              user_id?: string;
              display_name?: string;
            };
            if (!payload.user_id || payload.user_id === user.id) {
              return;
            }
            setTypingUsers((current) => {
              const next = { ...current };
              if (event.type === "typing.stop") {
                delete next[payload.user_id!];
              } else {
                next[payload.user_id!] = payload.display_name ?? "Someone";
              }
              return next;
            });
          }
        } catch {
          // Ignore malformed realtime payloads from this dev build.
        }
      };

      socket.onerror = () => {
        setRealtimeStatus("error");
      };

      socket.onclose = () => {
        if (stopped) {
          return;
        }
        setRealtimeStatus("disconnected");
        reconnectTimer = window.setTimeout(connect, 1500);
      };
    }

    connect();

    return () => {
      stopped = true;
      if (reconnectTimer !== undefined) {
        window.clearTimeout(reconnectTimer);
      }
      if (socketRef.current === socket) {
        socketRef.current = null;
      }
      socket?.close(1000, "workspace changed");
      setPresence({});
      setTypingUsers({});
    };
  }, [
    accessToken,
    queryClient,
    selectedChannelID,
    selectedWorkspaceID,
    user.id,
  ]);

  return (
    <section className="grid flex-1 gap-4 md:grid-cols-[260px_1fr_280px]">
      <aside className="rounded-2xl border border-slate-800 bg-slate-900/70 p-4">
        <div className="mb-4 rounded-xl bg-slate-950 p-3">
          <p className="text-sm text-slate-400">Signed in as</p>
          <p className="font-semibold">{user.display_name}</p>
          <p className="truncate text-xs text-slate-500">{user.email}</p>
          <div className="mt-3 flex items-center gap-2 text-xs text-slate-400">
            <span
              className={`h-2 w-2 rounded-full ${
                realtimeStatus === "connected"
                  ? "bg-emerald-400"
                  : realtimeStatus === "connecting"
                    ? "bg-yellow-400"
                    : "bg-slate-600"
              }`}
            />
            Realtime: {realtimeStatus}
          </div>
        </div>

        <h2 className="font-semibold">Workspaces</h2>
        <div className="mt-3 space-y-2">
          {workspacesQuery.data?.workspaces.map((workspace) => (
            <button
              key={workspace.id}
              className={`w-full rounded-xl px-4 py-2 text-left text-sm font-medium ${workspace.id === selectedWorkspaceID ? "bg-cyan-500 text-slate-950" : "bg-slate-950 text-slate-200 hover:bg-slate-800"}`}
              onClick={() => setSelectedWorkspaceID(workspace.id)}
            >
              {workspace.name}
            </button>
          ))}
        </div>

        <form
          className="mt-4 space-y-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (workspaceName.trim()) {
              createWorkspace.mutate(workspaceName.trim());
            }
          }}
        >
          <input
            className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 text-sm outline-none ring-cyan-500/30 focus:ring-4"
            placeholder="New workspace"
            value={workspaceName}
            onChange={(event) => setWorkspaceName(event.target.value)}
          />
          <button
            className="w-full rounded-xl bg-slate-800 px-3 py-2 text-sm hover:bg-slate-700"
            type="submit"
          >
            Create workspace
          </button>
        </form>
      </aside>

      <section className="flex min-h-[620px] flex-col rounded-2xl border border-slate-800 bg-slate-900/70 p-4">
        <div className="flex items-center justify-between border-b border-slate-800 pb-3">
          <div>
            <p className="text-xs uppercase tracking-[0.25em] text-slate-500">
              {activeWorkspace?.name ?? "No workspace"}
            </p>
            <h2 className="font-semibold">
              #{activeChannel?.name ?? "select-channel"}
            </h2>
          </div>
          <button
            className="rounded-xl bg-slate-800 px-3 py-2 text-xs text-slate-300 hover:bg-slate-700 disabled:opacity-50"
            disabled={!selectedChannelID || messagesQuery.isFetching}
            onClick={() => void messagesQuery.refetch()}
          >
            {messagesQuery.isFetching ? "Refreshing…" : "Refresh"}
          </button>
        </div>

        <div className="flex-1 space-y-3 overflow-y-auto py-4">
          {messagesQuery.data?.messages.length ? (
            messagesQuery.data.messages.map((message) => (
              <div key={message.id} className="rounded-xl bg-slate-950 p-3">
                <div className="mb-1 flex items-center justify-between text-xs text-slate-500">
                  <span className="font-semibold text-cyan-300">
                    {message.author_display_name}
                  </span>
                  <time>
                    {new Date(message.created_at).toLocaleTimeString()}
                  </time>
                </div>
                <p className="whitespace-pre-wrap text-sm text-slate-100">
                  {message.body}
                </p>
              </div>
            ))
          ) : (
            <div className="rounded-xl border border-dashed border-slate-700 p-8 text-center text-slate-400">
              {selectedChannelID
                ? "No messages yet. Send the first one."
                : "Create/select a workspace to start chatting."}
            </div>
          )}
        </div>

        <form
          className="flex gap-2 border-t border-slate-800 pt-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (
              messageBody.trim() &&
              selectedWorkspaceID &&
              selectedChannelID
            ) {
              sendMessage.mutate(messageBody.trim());
            }
          }}
        >
          <input
            className="flex-1 rounded-xl border border-slate-700 bg-slate-950 px-4 py-3 text-sm outline-none ring-cyan-500/30 focus:ring-4"
            placeholder={
              selectedChannelID
                ? "Message this channel"
                : "Select a text channel first"
            }
            value={messageBody}
            onChange={(event) => {
              setMessageBody(event.target.value);
              if (event.target.value.trim()) {
                sendTypingStart();
              } else {
                sendTypingStop();
              }
            }}
            disabled={!selectedChannelID}
          />
          <button
            className="rounded-xl bg-cyan-500 px-5 py-3 font-semibold text-slate-950 hover:bg-cyan-400 disabled:cursor-not-allowed disabled:opacity-60"
            type="submit"
            disabled={!selectedChannelID || sendMessage.isPending}
          >
            Send
          </button>
        </form>
        {typingNames.length ? (
          <p className="mt-2 text-xs text-cyan-300">
            {typingNames.join(", ")} {typingNames.length === 1 ? "is" : "are"}{" "}
            typing…
          </p>
        ) : null}
      </section>

      <aside className="rounded-2xl border border-slate-800 bg-slate-900/70 p-4">
        <h2 className="font-semibold">Online ({onlineUsers.length})</h2>
        <div className="mt-3 space-y-2">
          {onlineUsers.length ? (
            onlineUsers.map((onlineUser) => (
              <div
                key={onlineUser.user_id}
                className="flex items-center gap-2 rounded-xl bg-slate-950 px-3 py-2 text-sm text-slate-300"
              >
                <span className="h-2 w-2 rounded-full bg-emerald-400" />
                {onlineUser.display_name}
              </div>
            ))
          ) : (
            <div className="rounded-xl border border-dashed border-slate-700 p-3 text-sm text-slate-500">
              No presence yet.
            </div>
          )}
        </div>

        <h2 className="mt-6 font-semibold">Channels</h2>
        <div className="mt-3 space-y-2">
          {textChannels.map((channel) => (
            <button
              key={channel.id}
              className={`w-full rounded-xl px-4 py-2 text-left text-sm ${channel.id === selectedChannelID ? "bg-slate-700 text-white" : "bg-slate-950 text-slate-300 hover:bg-slate-800"}`}
              onClick={() => setSelectedChannelID(channel.id)}
            >
              # {channel.name}
            </button>
          ))}
          {voiceChannels.map((channel) => (
            <div
              key={channel.id}
              className="w-full rounded-xl border border-cyan-900/60 bg-cyan-950/20 px-4 py-2 text-left text-sm text-cyan-200"
            >
              🔊 {channel.name}
            </div>
          ))}
        </div>

        <h2 className="mt-6 font-semibold">
          Voice Rooms ({voiceChannels.length})
        </h2>
        <div className="mt-3 space-y-2">
          {voiceChannels.length ? (
            voiceChannels.map((channel) => (
              <div key={channel.id} className="rounded-xl bg-slate-950 p-4">
                <p className="font-medium">🔊 {channel.name}</p>
                <p className="mt-1 text-sm text-slate-400">
                  LiveKit voice room. Requires a configured LiveKit server.
                </p>
                {joinedVoiceChannelID === channel.id ? (
                  <>
                    <div className="mt-3 space-y-2">
                      {voiceParticipants.map((participant) => (
                        <div
                          key={participant.identity}
                          className="flex items-center gap-2 rounded-lg border border-cyan-900/50 bg-cyan-950/30 px-3 py-2 text-sm text-cyan-100"
                        >
                          <span className="h-2 w-2 rounded-full bg-emerald-400" />
                          <span className="min-w-0 flex-1 truncate">
                            {participant.name}
                          </span>
                          {participant.isLocal ? (
                            <span className="text-xs text-cyan-400">you</span>
                          ) : null}
                        </div>
                      ))}
                    </div>
                    <button
                      className="mt-3 w-full rounded-xl bg-red-500/20 px-3 py-2 text-sm text-red-200 hover:bg-red-500/30"
                      onClick={leaveVoice}
                    >
                      Leave voice
                    </button>
                  </>
                ) : (
                  <button
                    className="mt-3 w-full rounded-xl bg-cyan-500 px-3 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-400 disabled:opacity-60"
                    disabled={joinVoice.isPending}
                    onClick={() => joinVoice.mutate(channel.id)}
                  >
                    {joinVoice.isPending ? "Joining…" : "Join voice"}
                  </button>
                )}
              </div>
            ))
          ) : (
            <div className="rounded-xl border border-dashed border-slate-700 p-4 text-sm text-slate-400">
              No voice rooms yet.
            </div>
          )}
        </div>
        {voiceError ? (
          <div className="mt-3 rounded-xl border border-red-900 bg-red-950/40 p-3 text-xs text-red-200">
            {voiceError === "livekit_not_configured"
              ? "LiveKit is not configured yet. Set LIVEKIT_URL, LIVEKIT_API_KEY, and LIVEKIT_API_SECRET in backend/.env."
              : voiceError}
          </div>
        ) : null}
      </aside>
    </section>
  );
}
