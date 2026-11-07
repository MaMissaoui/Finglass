#![allow(dead_code)]

use std::io::{Cursor, Write};

use rust_decimal::Decimal;
use rust_decimal::prelude::FromStr;
use zip::write::SimpleFileOptions;

pub fn dec(value: &str) -> Decimal {
    Decimal::from_str(value).unwrap()
}

pub fn cp1252(text: &str) -> Vec<u8> {
    encoding_rs::WINDOWS_1252.encode(text).0.into_owned()
}

pub fn utf16le(text: &str) -> Vec<u8> {
    text.encode_utf16().flat_map(u16::to_le_bytes).collect()
}

pub fn utf16be(text: &str) -> Vec<u8> {
    text.encode_utf16().flat_map(u16::to_be_bytes).collect()
}

pub fn write_zip(entries: &[(&str, &[u8])]) -> Vec<u8> {
    let mut writer = zip::ZipWriter::new(Cursor::new(Vec::new()));
    for (name, content) in entries {
        writer.start_file(*name, SimpleFileOptions::default()).unwrap();
        writer.write_all(content).unwrap();
    }
    writer.finish().unwrap().into_inner()
}

pub fn stored_entry(name: &str, content: &str) -> Vec<u8> {
    let name_bytes = name.as_bytes();
    let content_bytes = content.as_bytes();
    let crc = crc32(content_bytes);
    let mut bytes = Vec::new();
    bytes.extend_from_slice(&0x0403_4b50u32.to_le_bytes());
    bytes.extend_from_slice(&20u16.to_le_bytes());
    bytes.extend_from_slice(&0u16.to_le_bytes());
    bytes.extend_from_slice(&0u16.to_le_bytes());
    bytes.extend_from_slice(&0u16.to_le_bytes());
    bytes.extend_from_slice(&0u16.to_le_bytes());
    bytes.extend_from_slice(&crc.to_le_bytes());
    bytes.extend_from_slice(&(content_bytes.len() as u32).to_le_bytes());
    bytes.extend_from_slice(&(content_bytes.len() as u32).to_le_bytes());
    bytes.extend_from_slice(&(name_bytes.len() as u16).to_le_bytes());
    bytes.extend_from_slice(&0u16.to_le_bytes());
    bytes.extend_from_slice(name_bytes);
    bytes.extend_from_slice(content_bytes);
    bytes
}

fn crc32(bytes: &[u8]) -> u32 {
    let mut crc = 0xffff_ffffu32;
    for byte in bytes {
        crc ^= u32::from(*byte);
        for _ in 0..8 {
            if crc & 1 != 0 {
                crc = (crc >> 1) ^ 0xedb8_8320;
            } else {
                crc >>= 1;
            }
        }
    }
    !crc
}
