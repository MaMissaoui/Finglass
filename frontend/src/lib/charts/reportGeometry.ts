/**
 * The geometry of the Reports page's tax-relevant meters. The Slint page draws
 * a 160 px track and fills it with the row's share of the largest category, so
 * the widest row always fills the track and the others compare against it.
 */

import { clampFraction } from './geometry';

/** The tax-relevant meter's track width, as in reports_page.slint. */
export const REPORT_METER_WIDTH = 160;

/** The track's height, as in reports_page.slint. */
export const REPORT_METER_HEIGHT = 6;

/** The fill width for one DTO fraction: a share of the fixed track. */
export function meterWidth(fraction: number, width = REPORT_METER_WIDTH): number {
  if (!(width > 0)) {
    return 0;
  }
  return Math.round(clampFraction(fraction) * width * 100) / 100;
}
