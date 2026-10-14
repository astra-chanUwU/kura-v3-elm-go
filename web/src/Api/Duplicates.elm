module Api.Duplicates exposing (groupsEndpoint, listGroups, listMembers, membersEndpoint)

{-| Public exact-copy review reads.

Both endpoints are uncredentialed public reads, so they work while the
browser is locked. Cursors are opaque server tokens: they are passed
back verbatim (percent-encoded for transport) and never parsed here,
so an invalid cursor is the server's 400 to report.
-}

import Domain.Duplicates exposing (DuplicateGroupsPage, DuplicateMembersPage, groupsPageDecoder, membersPageDecoder)
import Http
import Url
import Url.Builder


listGroups : String -> Maybe String -> Int -> (Result Http.Error DuplicateGroupsPage -> msg) -> Cmd msg
listGroups apiBase cursor limit toMsg =
    Http.request
        { method = "GET"
        , headers = []
        , body = Http.emptyBody
        , timeout = Just 30000
        , tracker = Nothing
        , url = groupsEndpoint apiBase cursor limit
        , expect = Http.expectJson toMsg groupsPageDecoder
        }


listMembers : String -> String -> Maybe String -> Int -> (Result Http.Error DuplicateMembersPage -> msg) -> Cmd msg
listMembers apiBase hash cursor limit toMsg =
    Http.request
        { method = "GET"
        , headers = []
        , body = Http.emptyBody
        , timeout = Just 30000
        , tracker = Nothing
        , url = membersEndpoint apiBase hash cursor limit
        , expect = Http.expectJson toMsg membersPageDecoder
        }


groupsEndpoint : String -> Maybe String -> Int -> String
groupsEndpoint apiBase cursor limit =
    let
        params =
            Url.Builder.int "limit" limit
                :: (case cursor of
                        Just value ->
                            [ Url.Builder.string "cursor" value ]

                        Nothing ->
                            []
                   )
    in
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "duplicates" ] params

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "duplicates" ] params


{-| The hash travels as one percent-encoded path segment (`sha256%3A…`)
plus an opaque, percent-encoded cursor when paging.
-}
membersEndpoint : String -> String -> Maybe String -> Int -> String
membersEndpoint apiBase hash cursor limit =
    let
        query =
            "limit="
                ++ String.fromInt limit
                ++ (case cursor of
                        Just value ->
                            "&cursor=" ++ Url.percentEncode value

                        Nothing ->
                            ""
                   )
    in
    base apiBase ++ "/api/posts/duplicates/" ++ Url.percentEncode hash ++ "?" ++ query


base : String -> String
base apiBase =
    let
        trimmed =
            String.trim apiBase
    in
    if String.endsWith "/" trimmed then
        String.dropRight 1 trimmed

    else
        trimmed
