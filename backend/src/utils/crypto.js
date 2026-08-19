const crypto = require('crypto');

function generateSignature(payload, secret) {
  return crypto.createHmac('sha256', secret).update(payload).digest('hex');
}

function verifySignature(payload, signature, secret) {
  const expectedSig = generateSignature(payload, secret);
  return crypto.timingSafeEqual(Buffer.from(signature), Buffer.from(expectedSig));
}

function generateSecret() {
  return crypto.randomBytes(32).toString('hex');
}

module.exports = { generateSignature, verifySignature, generateSecret };
