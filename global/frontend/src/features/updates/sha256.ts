// 本机 SHA-256，给 HTTP 页面在没有 Web Crypto 时校验包文件；不管上传。

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

function rotr(x: number, n: number) {
  return ((x >>> n) | (x << (32 - n))) >>> 0;
}

function hex(bytes: Uint8Array) {
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// 按块累加 SHA-256，算完得到 hex。
export class Sha256 {
  private h0 = 0x6a09e667;
  private h1 = 0xbb67ae85;
  private h2 = 0x3c6ef372;
  private h3 = 0xa54ff53a;
  private h4 = 0x510e527f;
  private h5 = 0x9b05688c;
  private h6 = 0x1f83d9ab;
  private h7 = 0x5be0cd19;
  private readonly block = new Uint8Array(64);
  private blockLen = 0;
  private size = 0;

  // 继续喂文件字节。
  update(chunk: Uint8Array) {
    let i = 0;
    while (i < chunk.length) {
      const n = Math.min(64 - this.blockLen, chunk.length - i);
      this.block.set(chunk.subarray(i, i + n), this.blockLen);
      this.blockLen += n;
      this.size += n;
      i += n;
      if (this.blockLen === 64) {
        this.compress();
        this.blockLen = 0;
      }
    }
  }

  // 补位后输出 64 位 hex。
  hex() {
    const n = this.size;
    this.update(new Uint8Array([0x80]));
    const pad = this.blockLen <= 56 ? 56 - this.blockLen : 120 - this.blockLen;
    if (pad > 0) this.update(new Uint8Array(pad));
    const len = new Uint8Array(8);
    const bitsHi = Math.floor(n / 0x20000000);
    const bitsLo = (n * 8) >>> 0;
    len[0] = (bitsHi >>> 24) & 0xff;
    len[1] = (bitsHi >>> 16) & 0xff;
    len[2] = (bitsHi >>> 8) & 0xff;
    len[3] = bitsHi & 0xff;
    len[4] = (bitsLo >>> 24) & 0xff;
    len[5] = (bitsLo >>> 16) & 0xff;
    len[6] = (bitsLo >>> 8) & 0xff;
    len[7] = bitsLo & 0xff;
    this.update(len);
    const words = [this.h0, this.h1, this.h2, this.h3, this.h4, this.h5, this.h6, this.h7];
    const out = new Uint8Array(32);
    for (let i = 0; i < 8; i++) {
      out[i * 4] = (words[i] >>> 24) & 0xff;
      out[i * 4 + 1] = (words[i] >>> 16) & 0xff;
      out[i * 4 + 2] = (words[i] >>> 8) & 0xff;
      out[i * 4 + 3] = words[i] & 0xff;
    }
    return hex(out);
  }

  private compress() {
    const w = new Uint32Array(64);
    const block = this.block;
    for (let i = 0; i < 16; i++) {
      w[i] = ((block[i * 4] << 24) | (block[i * 4 + 1] << 16) | (block[i * 4 + 2] << 8) | block[i * 4 + 3]) >>> 0;
    }
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
    }
    let a = this.h0;
    let b = this.h1;
    let c = this.h2;
    let d = this.h3;
    let e = this.h4;
    let f = this.h5;
    let g = this.h6;
    let h = this.h7;
    for (let i = 0; i < 64; i++) {
      const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      const ch = (e & f) ^ (~e & g);
      const temp1 = (h + S1 + ch + K[i] + w[i]) >>> 0;
      const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const temp2 = (S0 + maj) >>> 0;
      h = g;
      g = f;
      f = e;
      e = (d + temp1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (temp1 + temp2) >>> 0;
    }
    this.h0 = (this.h0 + a) >>> 0;
    this.h1 = (this.h1 + b) >>> 0;
    this.h2 = (this.h2 + c) >>> 0;
    this.h3 = (this.h3 + d) >>> 0;
    this.h4 = (this.h4 + e) >>> 0;
    this.h5 = (this.h5 + f) >>> 0;
    this.h6 = (this.h6 + g) >>> 0;
    this.h7 = (this.h7 + h) >>> 0;
  }
}
