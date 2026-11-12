import Foundation
import CoreFoundation

public enum JSONValue: Codable, Equatable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case object([String: JSONValue])
    case array([JSONValue])
    case null

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
        } else {
            self = .object(try container.decode([String: JSONValue].self))
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case let .string(value): try container.encode(value)
        case let .number(value): try container.encode(value)
        case let .bool(value): try container.encode(value)
        case let .object(value): try container.encode(value)
        case let .array(value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }
}

public extension Encodable {
    func skejJSONValue() throws -> JSONValue {
        let data = try JSONEncoder().encode(self)
        return try JSONDecoder().decode(JSONValue.self, from: data)
    }
}

public func makeJSONValue(from object: Any) throws -> JSONValue {
    switch object {
    case let value as String:
        return .string(value)
    case let value as NSNumber:
        if CFGetTypeID(value) == CFBooleanGetTypeID() {
            return .bool(value.boolValue)
        }
        return .number(value.doubleValue)
    case let value as Bool:
        return .bool(value)
    case let value as [Any]:
        return .array(try value.map(makeJSONValue(from:)))
    case let value as [String: Any]:
        return .object(try value.mapValues(makeJSONValue(from:)))
    default:
        return .null
    }
}
