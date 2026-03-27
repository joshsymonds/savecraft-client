using System;
using System.Collections.Concurrent;
using System.Net.WebSockets;
using System.Threading;
using System.Threading.Tasks;
using Google.Protobuf;
using RimWorld;
using Savecraft.V1;
using Message = Savecraft.V1.Message;
using SavecraftRimWorld.Collectors;
using Verse;

namespace SavecraftRimWorld.Connection
{
    public enum ConnectionStatus
    {
        Disconnected,
        Connecting,
        Connected,
        Linked
    }

    public enum SyncState
    {
        Idle,
        Syncing,
        Success,
        Error
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
        const int ReconnectMaxMs = 300000; // 5 minutes max between retries
        const int BackoffMultiplier = 2;
        const int MaxConsecutiveFailures = 10; // Stop retrying after 10 consecutive failures
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
        volatile SyncState syncState = SyncState.Idle;
        // These fields are written from background threads and read from the UI thread.
        // lastSectionCount is volatile for atomicity. DateTime (64-bit struct) can tear on
        // 32-bit runtimes but only produces a harmless nonsensical display for one frame.
        // lastSyncError is a reference type (atomic assignment on all .NET runtimes).
        DateTime lastSyncTime;
        volatile int lastSectionCount;
        string lastSyncError;

        public ConnectionStatus Status => status;
        public SyncState CurrentSyncState => syncState;
        public DateTime LastSyncTime => lastSyncTime;
        public int LastSectionCount => lastSectionCount;
        public string LastSyncError => lastSyncError;
        public ConcurrentQueue<Action> MainThreadQueue => mainThreadQueue;

        /// <summary>
        /// Reset sync state to Idle so a new sync can be visually triggered (e.g., Test Push).
        /// </summary>
        public void ResetSyncState()
        {
            syncState = SyncState.Idle;
        }

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
                Log.Warning("[Savecraft] Not connected, skipping push.");
                return;
            }

            if (collectorRunner == null)
            {
                Log.Warning("[Savecraft] No collector runner, skipping push.");
                return;
            }

            // Enqueue collector execution on main thread
            syncState = SyncState.Syncing;
            EnqueueMainThread(() =>
            {
                try
                {
                    var msg = collectorRunner.BuildPushSave();
                    if (msg != null)
                    {
                        lastSectionCount = msg.PushSave?.Sections.Count ?? 0;
                        // Send on background thread with error logging
                        Task.Run(async () =>
                        {
                            try { await SendAsync(msg); }
                            catch (Exception sendEx)
                            {
                                syncState = SyncState.Error;
                                lastSyncError = sendEx.Message;
                                Log.Error($"[Savecraft] Send failed: {sendEx}");
                            }
                        });
                    }
                    else
                    {
                        syncState = SyncState.Idle;
                    }
                }
                catch (Exception ex)
                {
                    syncState = SyncState.Error;
                    lastSyncError = ex.Message;
                    Log.Error($"[Savecraft] Failed to build push: {ex}");
                }
            });
        }

        /// <summary>
        /// Start the connection lifecycle. If no credentials exist, registers first.
        /// Then connects to /ws/daemon with the stored token.
        /// Safe to call multiple times (e.g., on game reload) — stops existing connection first.
        /// </summary>
        public void Start()
        {
            // Stop any existing connection loop before starting a new one.
            // FinalizeInit() calls Start() on every game load, so without this guard,
            // each load orphans the previous ConnectionLoop task and its socket.
            if (cts != null)
            {
                cts.Cancel();
                socket?.Dispose();
                socket = null;
                status = ConnectionStatus.Disconnected;
            }

            cts = new CancellationTokenSource();
            Task.Run(() => ConnectionLoop(cts.Token));
        }

        /// <summary>
        /// Gracefully disconnect. Sends SourceOffline before closing.
        /// RimWorld's GameComponent has no shutdown hook, so this is not called automatically.
        /// The server detects disconnection via WebSocket close/timeout. This method exists
        /// for explicit shutdown scenarios (e.g., future mod unload support).
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
                    Log.Warning($"[Savecraft] Error during disconnect: {ex.Message}");
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
                Log.Warning("[Savecraft] Send dropped, not connected.");
                return;
            }

            await SendImmediate(msg);
        }

        /// <summary>
        /// Request a new link code from the server (for re-pairing).
        /// Resets linked state so the new code will be displayed.
        /// </summary>
        public void RequestNewLinkCode()
        {
            if (status != ConnectionStatus.Connected && status != ConnectionStatus.Linked)
            {
                Log.Warning("[Savecraft] Not connected, cannot request new link code.");
                return;
            }

            settings.IsLinked = false;
            settings.LinkCode = "";
            settings.Write();
            status = ConnectionStatus.Connected;

            var msg = new Message { RefreshLinkCode = new RefreshLinkCode() };
            Task.Run(async () =>
            {
                try { await SendAsync(msg); }
                catch (Exception ex) { Log.Error($"[Savecraft] Failed to request link code: {ex}"); }
            });
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
                    Log.Error("[Savecraft] Registration failed, cannot connect.");
                    return;
                }
            }

            var delay = ReconnectBaseMs;
            var consecutiveFailures = 0;

            while (!ct.IsCancellationRequested)
            {
                try
                {
                    status = ConnectionStatus.Connecting;
                    await Connect(ct);

                    // Reset backoff and failure count on successful connection
                    delay = ReconnectBaseMs;
                    consecutiveFailures = 0;
                    status = settings.IsLinked ? ConnectionStatus.Linked : ConnectionStatus.Connected;

                    Log.Message("[Savecraft] Connected to server.");
                    await ReceiveLoop(ct);
                }
                catch (OperationCanceledException) when (ct.IsCancellationRequested)
                {
                    return;
                }
                catch (Exception ex)
                {
                    Log.Warning($"[Savecraft] Connection lost: {ex.Message}");
                    consecutiveFailures++;
                }

                // Clean up old socket
                socket?.Dispose();
                socket = null;
                status = ConnectionStatus.Disconnected;

                if (ct.IsCancellationRequested) return;

                if (consecutiveFailures >= MaxConsecutiveFailures)
                {
                    Log.Error($"[Savecraft] {MaxConsecutiveFailures} consecutive connection failures, giving up. Restart the game to retry.");
                    return;
                }

                Log.Warning($"[Savecraft] Reconnecting in {delay / 1000}s...");
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
            Log.Message("[Savecraft] Registering new source...");
            status = ConnectionStatus.Connecting;

            var registerUrl = settings.ServerUrl.TrimEnd('/') + "/ws/register";
            Log.Message($"[Savecraft] Registration endpoint: {registerUrl}");

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
                    Log.Error($"[Savecraft] Registration connect failed: {ex.Message}");
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

                        // Persist credentials and notify player
                        EnqueueMainThread(() =>
                        {
                            settings.Write();
                            Log.Message($"[Savecraft] Registered! Link code: {reg.LinkCode}");
                            Find.LetterStack.ReceiveLetter(
                                "Savecraft Registered",
                                $"Your link code is: {reg.LinkCode}\n\nEnter this code at savecraft.gg to connect your colony to Savecraft.",
                                LetterDefOf.NeutralEvent);
                        });
                    }
                    else
                    {
                        Log.Error($"[Savecraft] Unexpected registration response: {responseMsg.PayloadCase}");
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
                    Version = SavecraftMod.Version,
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
            var ms = new System.IO.MemoryStream();

            while (!ct.IsCancellationRequested && socket?.State == WebSocketState.Open)
            {
                // Accumulate multi-frame messages
                ms.SetLength(0);
                {
                    WebSocketReceiveResult result;
                    do
                    {
                        result = await socket.ReceiveAsync(new ArraySegment<byte>(buffer), ct);
                        if (result.MessageType == WebSocketMessageType.Close)
                        {
                            Log.Message("[Savecraft] Server closed connection.");
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
                        Log.Warning($"[Savecraft] Failed to parse message: {ex.Message}");
                    }
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
                        var alreadyLinked = settings.IsLinked;
                        settings.IsLinked = true;
                        settings.LinkCode = "";
                        settings.Write();
                        status = ConnectionStatus.Linked;

                        if (alreadyLinked)
                        {
                            Log.Message("[Savecraft] Duplicate SourceLinked received, suppressing notification.");
                            return;
                        }

                        Log.Message("[Savecraft] Source linked to user account!");
                        Find.LetterStack.ReceiveLetter(
                            "Savecraft Linked",
                            "Your colony is now linked to your Savecraft account!\n\nColony data will sync automatically on each save.",
                            LetterDefOf.PositiveEvent);
                    });
                    break;

                case Message.PayloadOneofCase.RefreshLinkCodeResult:
                    var linkResult = msg.RefreshLinkCodeResult;
                    EnqueueMainThread(() =>
                    {
                        if (settings.IsLinked)
                        {
                            Log.Message("[Savecraft] Ignoring stale link code, already linked.");
                            return;
                        }

                        if (settings.LinkCode == linkResult.LinkCode)
                        {
                            Log.Message("[Savecraft] Duplicate link code received, suppressing notification.");
                            return;
                        }

                        settings.LinkCode = linkResult.LinkCode;
                        settings.Write();
                        Log.Message($"[Savecraft] New link code: {linkResult.LinkCode}");
                        Find.LetterStack.ReceiveLetter(
                            "Savecraft Link Code",
                            $"Your new link code is: {linkResult.LinkCode}\n\nEnter this code at savecraft.gg to link your colony.",
                            LetterDefOf.NeutralEvent);
                    });
                    break;

                case Message.PayloadOneofCase.PushSaveResult:
                    var pushResult = msg.PushSaveResult;
                    syncState = SyncState.Success;
                    lastSyncTime = DateTime.UtcNow;
                    Log.Message($"[Savecraft] Save pushed successfully (save_uuid: {pushResult.SaveUuid}).");
                    break;

                default:
                    Log.Message($"[Savecraft] Received: {msg.PayloadCase}");
                    break;
            }
        }

        void EnqueueMainThread(Action action)
        {
            mainThreadQueue.Enqueue(action);
        }
    }
}
