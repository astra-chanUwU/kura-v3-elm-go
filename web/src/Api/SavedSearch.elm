module Api.SavedSearch exposing (SavedSearch, VisibleItem, create, decoder, delete, list, listDecoder, update, visible)

{-| Server-backed saved searches (`/api/saved-searches`).

The server owns the list; each entry has a stable id plus the human
name and the KuraQL query. Reads are owner-scoped, so every request —
including `list` — carries the unlocked credential (`Nothing` while
open-local, where the `local` actor still resolves). A 401 means the
credential is missing or dead: callers fall back to the on-device list
and never merge the two owners.
-}

import Api.Access exposing (authHeaders)
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


list : String -> Maybe String -> (Result Http.Error (List SavedSearch) -> msg) -> Cmd msg
list apiBase credential toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders credential
        , url = endpoint apiBase []
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg listDecoder
        , timeout = Nothing
        , tracker = Nothing
        }


create : String -> Maybe String -> String -> String -> (Result Http.Error SavedSearch -> msg) -> Cmd msg
create apiBase credential name query toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase []
        , body =
            Http.jsonBody
                (Encode.object
                    [ ( "name", Encode.string name )
                    , ( "query", Encode.string query )
                    ]
                )
        , expect = Http.expectJson toMsg decoder
        , timeout = Nothing
        , tracker = Nothing
        }


update : String -> Maybe String -> String -> Maybe String -> Maybe String -> (Result Http.Error SavedSearch -> msg) -> Cmd msg
update apiBase credential id maybeName maybeQuery toMsg =
    Http.request
        { method = "PATCH"
        , headers = authHeaders credential
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


delete : String -> Maybe String -> String -> (Result Http.Error () -> msg) -> Cmd msg
delete apiBase credential id toMsg =
    Http.request
        { method = "DELETE"
        , headers = authHeaders credential
        , url = endpoint apiBase [ id ]
        , body = Http.emptyBody
        , expect = Http.expectWhatever toMsg
        , timeout = Nothing
        , tracker = Nothing
        }


{-| One navigator row. `id` is the server id, or the query itself when the
on-device fallback list is active; `label` is the query to run.
-}
type alias VisibleItem =
    { id : String
    , label : String
    }


{-| Owner separation in one place: the local fallback shows only the
on-device queries, the server mode shows only the authenticated owner's
entries. The two lists are never merged.
-}
visible : Bool -> List SavedSearch -> List String -> List VisibleItem
visible localFallback server local =
    if localFallback then
        List.map (\query -> { id = query, label = query }) local

    else
        List.map (\savedSearch -> { id = savedSearch.id, label = savedSearch.query }) server


endpoint : String -> List String -> String
endpoint apiBase segments =
    if String.trim apiBase == "" then
        Url.Builder.absolute ([ "api", "saved-searches" ] ++ segments) []

    else
        Url.Builder.crossOrigin apiBase ([ "api", "saved-searches" ] ++ segments) []
