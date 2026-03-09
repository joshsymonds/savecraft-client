using System;
using System.Collections.Concurrent;
using System.Net.WebSockets;
using System.Threading;
using System.Threading.Tasks;
using Google.Protobuf;
using Savecraft.V1;
using SavecraftRimWorld.Collectors;

namespace SavecraftRimWorld.Connection
{
    public enum ConnectionStatus
    {
        Disconnected,
        Connecting,
        Connected,
        Linked
    }

    /// <summary>
    /// Manages the WebSocket connection to Savecraft's server.
    /// Handles registration, authentication, reconnection, and message dispatch.
    /// All WebSocket I/O runs on background threads. Incoming messages are queued
    /// for main-thread processing via <see cref="MainThreadQueue"/>.
    /// </summary>
    public class SavecraftConnection
    {
        const int ReconnectBaseMs = 1000;
        const int ReconnectMaxMs = 60000;
        const int BackoffMultiplier = 2;
        const int DialTimeoutMs = 10000;
        const int WriteTimeoutMs = 5000;
        const int ReceiveBufferSize = 65536;

        readonly SavecraftSettings settings;
        readonly ConcurrentQueue<Action> mainThreadQueue;
        readonly SemaphoreSlim sendLock = new SemaphoreSlim(1, 1);
        CollectorRunner collectorRunner;

        ClientWebSocket socket;
        CancellationTokenSource cts;
        volatile ConnectionStatus status = ConnectionStatus.Disconnected;

        public ConnectionStatus Status => status;
        public ConcurrentQueue<Action> MainThreadQueue => mainThreadQueue;

        public SavecraftConnection(SavecraftSettings settings)
        {
            this.settings = settings;
            this.mainThreadQueue = new ConcurrentQueue<Action>();
        }

        /// <summary>
        /// Set the collector runner. Must be called before OnSave() will work.
        /// </summary>
        public void SetCollectorRunner(CollectorRunner runner)
        {
            collectorRunner = runner;
        }

        /// <summary>
        /// Called from the save hook (Harmony patch). Enqueues collector execution
        /// on the main thread, then sends the result on the background thread.
        /// </summary>
        public void OnSave()
        {
            if (status != ConnectionStatus.Connected && status != ConnectionStatus.Linked)
            {
                Verse.Log.Warning("[Savecraft] Not connected, skipping push.");
                return;
            }

            if (collectorRunner == null)
            {
                Verse.Log.Warning("[Savecraft] No collector runner, skipping push.");
                return;
            }

            // Enqueue collector execution on main thread
            EnqueueMainThread(() =>
            {
                try
                {
                    var msg = collectorRunner.BuildPushSave();
                    if (msg != null)
                    {
                        // Send on background thread with error logging
                        Task.Run(async () =>
                        {
                            try { await SendAsync(msg); }
                            catch (Exception sendEx) { Verse.Log.Error($"[Savecraft] Send failed: {sendEx}"); }
                        });
                    }
                }
                catch (Exception ex)
                {
                    Verse.Log.Error($"[Savecraft] Failed to build push: {ex}");
                }
            });
        }

        /// <summary>
        /// Start the connection lifecycle. If no credentials exist, registers first.
        /// Then connects to /ws/daemon with the stored token.
        /// </summary>
        public void Start()
        {
            cts = new CancellationTokenSource();
            Task.Run(() => ConnectionLoop(cts.Token));
        }

        /// <summary>
        /// Gracefully disconnect. Sends SourceOffline before closing.
        /// </summary>
        public void Stop()
        {
            cts?.Cancel();

            if (socket?.State == WebSocketState.Open)
            {
                try
                {
                    var msg = new Message
                    {
                        SourceOffline = new SourceOffline
                        {
                            Timestamp = Google.Protobuf.WellKnownTypes.Timestamp.FromDateTime(DateTime.UtcNow)
                        }
                    };
                    SendImmediate(msg).Wait(TimeSpan.FromSeconds(2));
                    socket.CloseAsync(WebSocketCloseStatus.NormalClosure, "shutdown",
                        CancellationToken.None).Wait(TimeSpan.FromSeconds(2));
                }
                catch (Exception ex)
                {
                    Verse.Log.Warning($"[Savecraft] Error during disconnect: {ex.Message}");
                }
            }

            socket?.Dispose();
            socket = null;
            status = ConnectionStatus.Disconnected;
        }

        /// <summary>
        /// Send a protobuf Message over the WebSocket. Drops silently if disconnected.
        /// </summary>
        public async Task SendAsync(Message msg)
        {
            if (socket?.State != WebSocketState.Open)
            {
                Verse.Log.Warning("[Savecraft] Send dropped, not connected.");
                return;
            }

            await SendImmediate(msg);
        }

        async Task SendImmediate(Message msg)
        {
            await sendLock.WaitAsync();
            try
            {
                var data = msg.ToByteArray();
                using (var writeTimeout = new CancellationTokenSource(WriteTimeoutMs))
                {
                    await socket.SendAsync(new ArraySegment<byte>(data), WebSocketMessageType.Binary,
                        true, writeTimeout.Token);
                }
            }
            finally
            {
                sendLock.Release();
            }
        }

        async Task ConnectionLoop(CancellationToken ct)
        {
            // Register if we don't have credentials yet
            if (!settings.HasCredentials)
            {
                await Register(ct);
                if (!settings.HasCredentials)
                {
                    Verse.Log.Error("[Savecraft] Registration failed, cannot connect.");
                    return;
                }
            }

            var delay = ReconnectBaseMs;

            while (!ct.IsCancellationRequested)
            {
                try
                {
                    status = ConnectionStatus.Connecting;
                    await Connect(ct);

                    // Reset backoff on successful connection
                    delay = ReconnectBaseMs;
                    status = settings.IsLinked ? ConnectionStatus.Linked : ConnectionStatus.Connected;

                    Verse.Log.Message("[Savecraft] Connected to server.");
                    await ReceiveLoop(ct);
                }
                catch (OperationCanceledException) when (ct.IsCancellationRequested)
                {
                    return;
                }
                catch (Exception ex)
                {
                    Verse.Log.Warning($"[Savecraft] Connection lost: {ex.Message}");
                }

                // Clean up old socket
                socket?.Dispose();
                socket = null;
                status = ConnectionStatus.Disconnected;

                if (ct.IsCancellationRequested) return;

                Verse.Log.Warning($"[Savecraft] Reconnecting in {delay}ms...");
                try
                {
                    await Task.Delay(delay, ct);
                }
                catch (OperationCanceledException)
                {
                    return;
                }

                delay = Math.Min(delay * BackoffMultiplier, ReconnectMaxMs);
            }
        }

        async Task Register(CancellationToken ct)
        {
            Verse.Log.Message("[Savecraft] Registering new source...");
            status = ConnectionStatus.Connecting;

            var registerUrl = settings.ServerUrl.TrimEnd('/') + "/ws/register";

            using (var ws = new ClientWebSocket())
            using (var dialTimeout = CancellationTokenSource.CreateLinkedTokenSource(ct))
            {
                dialTimeout.CancelAfter(DialTimeoutMs);

                try
                {
                    await ws.ConnectAsync(new Uri(registerUrl), dialTimeout.Token);
                }
                catch (Exception ex)
                {
                    Verse.Log.Error($"[Savecraft] Registration connect failed: {ex.Message}");
                    status = ConnectionStatus.Disconnected;
                    return;
                }

                // Send Register message
                var registerMsg = new Message
                {
                    Register = new Register
                    {
                        Hostname = Environment.MachineName,
                        Os = "RimWorld",
                        Arch = "Unity"
                    }
                };

                var data = registerMsg.ToByteArray();
                await ws.SendAsync(new ArraySegment<byte>(data), WebSocketMessageType.Binary,
                    true, dialTimeout.Token);

                // Receive RegisterResult
                var buffer = new byte[ReceiveBufferSize];
                var result = await ws.ReceiveAsync(new ArraySegment<byte>(buffer), dialTimeout.Token);

                if (result.MessageType == WebSocketMessageType.Binary)
                {
                    var responseMsg = Message.Parser.ParseFrom(buffer, 0, result.Count);
                    if (responseMsg.PayloadCase == Message.PayloadOneofCase.RegisterResult)
                    {
                        var reg = responseMsg.RegisterResult;
                        settings.SourceUuid = reg.SourceUuid;
                        settings.SourceToken = reg.SourceToken;
                        settings.LinkCode = reg.LinkCode;
                        settings.IsLinked = false;

                        // Persist credentials immediately
                        EnqueueMainThread(() =>
                        {
                            settings.Write();
                            Verse.Log.Message($"[Savecraft] Registered! Link code: {reg.LinkCode}");
                            Verse.Log.Message($"[Savecraft] Enter this code at savecraft.gg to link your colony.");
                        });
                    }
                    else
                    {
                        Verse.Log.Error($"[Savecraft] Unexpected registration response: {responseMsg.PayloadCase}");
                    }
                }

                await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, "registered",
                    CancellationToken.None);
            }

            status = ConnectionStatus.Disconnected;
        }

        async Task Connect(CancellationToken ct)
        {
            var daemonUrl = settings.ServerUrl.TrimEnd('/') + "/ws/daemon";

            socket = new ClientWebSocket();
            socket.Options.SetRequestHeader("Authorization", $"Bearer {settings.SourceToken}");

            using (var dialTimeout = CancellationTokenSource.CreateLinkedTokenSource(ct))
            {
                dialTimeout.CancelAfter(DialTimeoutMs);
                await socket.ConnectAsync(new Uri(daemonUrl), dialTimeout.Token);
            }

            // Send SourceOnline
            var onlineMsg = new Message
            {
                SourceOnline = new SourceOnline
                {
                    Version = "0.1.0",
                    Timestamp = Google.Protobuf.WellKnownTypes.Timestamp.FromDateTime(DateTime.UtcNow),
                    Platform = "rimworld",
                    Os = Environment.OSVersion.ToString(),
                    Arch = Environment.Is64BitOperatingSystem ? "x64" : "x86",
                    Hostname = Environment.MachineName
                }
            };

            await SendImmediate(onlineMsg);
        }

        async Task ReceiveLoop(CancellationToken ct)
        {
            var buffer = new byte[ReceiveBufferSize];

            while (!ct.IsCancellationRequested && socket?.State == WebSocketState.Open)
            {
                // Accumulate multi-frame messages
                var ms = new System.IO.MemoryStream();
                WebSocketReceiveResult result;
                do
                {
                    result = await socket.ReceiveAsync(new ArraySegment<byte>(buffer), ct);
                    if (result.MessageType == WebSocketMessageType.Close)
                    {
                        Verse.Log.Message("[Savecraft] Server closed connection.");
                        return;
                    }
                    ms.Write(buffer, 0, result.Count);
                }
                while (!result.EndOfMessage);

                if (result.MessageType != WebSocketMessageType.Binary) continue;

                try
                {
                    var msg = Message.Parser.ParseFrom(ms.GetBuffer(), 0, (int)ms.Length);
                    HandleMessage(msg);
                }
                catch (Exception ex)
                {
                    Verse.Log.Warning($"[Savecraft] Failed to parse message: {ex.Message}");
                }
            }
        }

        void HandleMessage(Message msg)
        {
            switch (msg.PayloadCase)
            {
                case Message.PayloadOneofCase.SourceLinked:
                    EnqueueMainThread(() =>
                    {
                        settings.IsLinked = true;
                        settings.LinkCode = "";
                        settings.Write();
                        status = ConnectionStatus.Linked;
                        Verse.Log.Message("[Savecraft] Source linked to user account!");
                    });
                    break;

                case Message.PayloadOneofCase.RefreshLinkCodeResult:
                    var linkResult = msg.RefreshLinkCodeResult;
                    EnqueueMainThread(() =>
                    {
                        settings.LinkCode = linkResult.LinkCode;
                        settings.Write();
                        Verse.Log.Message($"[Savecraft] Link code: {linkResult.LinkCode}");
                    });
                    break;

                case Message.PayloadOneofCase.PushSaveResult:
                    var pushResult = msg.PushSaveResult;
                    Verse.Log.Message($"[Savecraft] Save pushed successfully (save_uuid: {pushResult.SaveUuid}).");
                    break;

                default:
                    Verse.Log.Message($"[Savecraft] Received: {msg.PayloadCase}");
                    break;
            }
        }

        void EnqueueMainThread(Action action)
        {
            mainThreadQueue.Enqueue(action);
        }
    }
}
