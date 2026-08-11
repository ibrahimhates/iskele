/**
 * Renders a string as a QR code, as a PNG data URI.
 *
 * The encoder is bundled rather than fetched, like everything else here: a
 * panel that only works with an internet connection is not much use on a
 * machine whose Docker daemon is the reason it exists. It is imported on
 * demand, though — it is 40 kB that only the two-factor enrollment panel ever
 * needs, and most sessions never open it.
 *
 * Errors are swallowed into null. The one caller shows the same value as text
 * beside the image, so a failed render costs the convenience of scanning and
 * nothing else.
 */
export async function qrDataURL(value: string): Promise<string | null> {
  try {
    const { default: QRCode } = await import('qrcode');
    return await QRCode.toDataURL(value, {
      errorCorrectionLevel: 'M',
      margin: 1,
      width: 240,
    });
  } catch {
    return null;
  }
}
