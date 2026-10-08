module Api.SavedSearch exposing (SavedSearch, create, decoder, delete, list, listDecoder, update)

{-| Server-backed saved searches (`/api/saved-searches`).

The server owns the list; each entry has a stable id plus the human
name and the KuraQL query. This app has no bearer-token plumbing, so
requests carry no `Authorization` header: they succeed against a local
dev server (no deployment token configured, `local` actor) and fail
with 401 where a token is configured (`system` actor). Callers must
treat 401 as "fall back to the on-device list".
-}

import Http
import Json.Decode as Decode exposing (Decoder)
import Json.Encode as Encode
import String
import Url.Builder


type alias SavedSearch =
    { id : String
    , name : String
    , query : String
    , createdAt : String
    , updatedAt : String
    }


decoder : Decoder SavedSearch
decoder =
    Decode.map5 SavedSearch
        (Decode.field "id" Decode.string)
        (Decode.field "name" Decode.string)
        (Decode.field "query" Decode.string)
        (Decode.oneOf [ Decode.field "created_at" Decode.string, Decode.succeed "" ])
        (Decode.oneOf [ Decode.field "updated_at" Decode.string, Decode.succeed "" ])


listDecoder : Decoder (List SavedSearch)
listDecoder =
    Decode.field "saved_searches" (Decode.list decoder)


list : String -> (Result Http.Error (List SavedSearch) -> msg) -> Cmd msg
list apiBase toMsg =
    Http.get { url = endpoint apiBase [], expect = Http.expectJson toMsg listDecoder }


create : String -> String -> String -> (Result Http.Error SavedSearch -> msg) -> Cmd msg
create apiBase name query toMsg =
    Http.post
        { url = endpoint apiBase []
        , body =
            Http.jsonBody
                (Encode.object
                    [ ( "name", Encode.string name )
                    , ( "query", Encode.string query )
                    ]
                )
        , expect = Http.expectJson toMsg decoder
        }


update : String -> String -> Maybe String -> Maybe String -> (Result Http.Error SavedSearch -> msg) -> Cmd msg
update apiBase id maybeName maybeQuery toMsg =
    Http.request
        { method = "PATCH"
        , headers = []
        , url = endpoint apiBase [ id ]
        , body =
            Http.jsonBody
                (Encode.object
                    (List.filterMap identity
                        [ Maybe.map (\name -> ( "name", Encode.string name )) maybeName
                        , Maybe.map (\query -> ( "query", Encode.string query )) maybeQuery
                        ]
                    )
                )
        , expect = Http.expectJson toMsg decoder
        , timeout = Nothing
        , tracker = Nothing
        }


delete : String -> String -> (Result Http.Error () -> msg) -> Cmd msg
delete apiBase id toMsg =
    Http.request
        { method = "DELETE"
        , headers = []
        , url = endpoint apiBase [ id ]
        , body = Http.emptyBody
        , expect = Http.expectWhatever toMsg
        , timeout = Nothing
        , tracker = Nothing
        }


endpoint : String -> List String -> String
endpoint apiBase segments =
    if String.trim apiBase == "" then
        Url.Builder.absolute ([ "api", "saved-searches" ] ++ segments) []

    else
        Url.Builder.crossOrigin apiBase ([ "api", "saved-searches" ] ++ segments) []
