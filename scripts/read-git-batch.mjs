import { mkdir, readFile, appendFile, open } from 'node:fs/promises';
import { once } from 'node:events';
import { spawn } from 'node:child_process';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';

const maxHeaderLength = 4096;
const maxChunkLength = 65536;

const [, , repository, requestPath, outputDirectory] = process.argv;
if (!repository || !requestPath || !outputDirectory || process.argv.length !== 5) {
    console.error('usage: read-git-batch.mjs <repository> <request-file> <output-directory>');
    process.exit(1);
}

const chunkLogPath = process.env.BOUNDARY_CHUNK_LOG;

function failure(message) {
    throw new Error(message);
}

async function writeChunk(file, chunk) {
    let offset = 0;
    while (offset < chunk.length) {
        const { bytesWritten } = await file.write(chunk, offset, chunk.length - offset);
        if (!Number.isInteger(bytesWritten) || bytesWritten <= 0 || bytesWritten > chunk.length - offset) {
            failure('Git failed while writing batch object');
        }
        offset += bytesWritten;
    }
}

async function readRequests() {
    const text = await readFile(requestPath, 'utf8');
    const lines = text.split('\n');
    if (lines.at(-1) === '') lines.pop();
    for (const object of lines) {
        if (!/^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$/.test(object)) {
            failure(`Git returned a malformed requested object: ${object}`);
        }
    }
    return lines;
}

async function run() {
    const requests = await readRequests();
    await mkdir(outputDirectory, { recursive: true });

    const child = spawn('git', ['-C', repository, 'cat-file', '--batch'], {
        stdio: ['pipe', 'pipe', 'pipe'],
    });
    child.stderr.resume();

    let childError;
    child.once('error', (error) => {
        childError = error;
    });
    const closePromise = once(child, 'close');
    const output = child.stdout[Symbol.asyncIterator]();
    let buffer = Buffer.alloc(0);
    let ended = false;

    async function pull() {
        if (ended) return;
        const result = await output.next();
        if (result.done) {
            ended = true;
            return;
        }
        if (result.value.length > 0) {
            buffer = buffer.length === 0 ? result.value : Buffer.concat([buffer, result.value]);
        }
    }

    async function readHeader(requested) {
        while (true) {
            const newline = buffer.indexOf(0x0a);
            if (newline >= 0) {
                const line = buffer.subarray(0, newline);
                buffer = buffer.subarray(newline + 1);
                if (line.length > maxHeaderLength || line.some((byte) => byte < 0x20 || byte > 0x7e)) {
                    failure(`Git returned a malformed batch response for object: ${requested}`);
                }
                return line;
            }
            if (buffer.length > maxHeaderLength) {
                failure(`Git returned a malformed batch response for object: ${requested}`);
            }
            if (ended) {
                failure(`Git returned a malformed batch response for object: ${requested}`);
            }
            await pull();
        }
    }

    async function ensureByte(requested) {
        while (buffer.length === 0 && !ended) await pull();
        if (buffer.length === 0) {
            failure(`Git returned truncated batch object: ${requested}`);
        }
    }

    async function ensureBytes(requested, count) {
        while (buffer.length < count && !ended) await pull();
        if (buffer.length < count) {
            failure(`Git returned truncated batch object: ${requested}`);
        }
    }

    async function parseResponse(requested) {
        const line = await readHeader(requested);
        const text = line.toString('ascii');
        const match = /^(\S+) (\S+) (\S+)$/.exec(text);
        if (!match || !/^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$/.test(match[1])) {
            failure(`Git returned a malformed batch response for object: ${requested}`);
        }
        const [, object, type, sizeText] = match;
        if (object !== requested) {
            failure('Git returned a malformed batch object response');
        }
        if (type !== 'blob') {
            failure(`Git returned non-blob object: ${requested}`);
        }
        if (!/^[0-9]+$/.test(sizeText)) {
            failure(`Git returned malformed blob size: ${requested}`);
        }
        let size;
        try {
            size = BigInt(sizeText);
        } catch {
            failure(`Git returned malformed blob size: ${requested}`);
        }

        const destination = `${outputDirectory}/${object.toLowerCase()}`;
        let file;
        let fileError;
        try {
            file = await open(destination, 'w');
            let remaining = size;
            while (remaining > 0n) {
                const length = remaining > BigInt(maxChunkLength)
                    ? maxChunkLength
                    : Number(remaining);
                await ensureBytes(requested, length);
                const chunk = buffer.subarray(0, length);
                buffer = buffer.subarray(length);
                await writeChunk(file, chunk);
                if (chunkLogPath) await appendFile(chunkLogPath, `${object} ${chunk.length}\n`);
                remaining -= BigInt(length);
            }
        } catch (error) {
            fileError = error;
        } finally {
            if (file) {
                try {
                    await file.close();
                } catch (error) {
                    fileError ??= error;
                }
            }
        }
        if (fileError) {
            if (fileError instanceof Error && fileError.message.startsWith('Git returned')) throw fileError;
            failure(`Git failed while reading batch object: ${requested}`);
        }

        await ensureByte(requested);
        const terminator = buffer[0];
        buffer = buffer.subarray(1);
        if (terminator !== 0x0a) {
            failure('Git returned malformed batch object terminator');
        }
    }

    async function parseResponses() {
        for (const requested of requests) await parseResponse(requested);
        while (true) {
            if (buffer.length > 0) failure('Git returned unexpected extra batch output');
            if (ended) break;
            await pull();
        }
    }

    let cleanupPromise;
    const terminate = async () => {
        cleanupPromise ??= (async () => {
            child.stdin.destroy();
            if (child.exitCode === null && !child.killed) child.kill();
            try {
                await closePromise;
            } catch {
                // The close event is still the reap boundary for a failed child.
            }
        })();
        return cleanupPromise;
    };

    const parser = parseResponses();
    const writer = pipeline(
        Readable.from(requests.map((object) => Buffer.from(`${object}\n`))),
        child.stdin,
    );
    let operationError;
    try {
        await Promise.all([parser, writer]);
    } catch (error) {
        operationError = error;
        await terminate();
    }
    await Promise.allSettled([parser, writer]);

    let closeResult;
    try {
        closeResult = await closePromise;
    } catch {
        closeResult = [null, null];
    }
    if (operationError) throw operationError;
    if (childError || closeResult[0] !== 0) failure('Git failed while reading batch objects');
}

try {
    await run();
} catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
}
