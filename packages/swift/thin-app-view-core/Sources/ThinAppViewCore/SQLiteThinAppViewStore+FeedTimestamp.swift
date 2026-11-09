import Foundation

extension SQLiteThinAppViewStore {
  /// The same built-in expression is used in indexes, ORDER BY, and seek bounds.
  /// Normalize the whole second separately so SQLite never rounds a fractional
  /// .999999 value into the next second. Preserve up to nine fractional digits;
  /// our writers/importer store six, and older databases stored whole seconds.
  static func feedTimestampSQL(_ column: String) -> String {
    let zone = "CASE WHEN substr(\(column),-1)='Z' THEN 'Z' ELSE substr(\(column),-6) END"
    let fraction = "CASE WHEN substr(\(column),20,1)='.' THEN substr(\(column),21,length(\(column))-(CASE WHEN substr(\(column),-1)='Z' THEN 21 ELSE 26 END)) ELSE '' END"
    return "(strftime('%Y-%m-%dT%H:%M:%S',substr(\(column),1,19)||(\(zone)))||'.'||substr((\(fraction))||'000000000',1,9)||'Z')"
  }

  static func feedTimestampKey(_ date: Date) -> String {
    String(ThinAppViewCursor.timestamp(date).dropLast()) + "000Z"
  }
}
