module Api.Post exposing (detail, editTags, revertTags, editReactions, search, mediaUrl)

import Domain.Post exposing (PostDetail, SearchResponse, TagEditResponse, TagEditTarget, ReactionResponse, ReactionTarget, detailDecoder, responseDecoder, tagEditResponseDecoder, reactionResponseDecoder)
import Http
import Json.Encode as Encode
import String
import Url.Builder


search : String -> String -> Maybe String -> Int -> (Result Http.Error SearchResponse -> msg) -> Cmd msg
search apiBase query cursor limit toMsg =
    Http.get
        { url = endpoint apiBase query cursor limit
        , expect = Http.expectJson toMsg responseDecoder
        }


detail : String -> String -> (Result Http.Error PostDetail -> msg) -> Cmd msg
detail apiBase postId toMsg =
    Http.get
        { url = detailEndpoint apiBase postId
        , expect = Http.expectJson toMsg detailDecoder
        }


editTags : String -> List TagEditTarget -> List String -> List String -> (Result Http.Error TagEditResponse -> msg) -> Cmd msg
editTags apiBase targets add remove toMsg =
    Http.post
        { url = tagsEndpoint apiBase
        , body =
            Http.jsonBody
                (Encode.object
                    [ ( "posts", Encode.list encodeTarget targets )
                    , ( "add", Encode.list Encode.string add )
                    , ( "remove", Encode.list Encode.string remove )
                    ]
                )
        , expect = Http.expectJson toMsg tagEditResponseDecoder
        }


revertTags : String -> List TagEditTarget -> Int -> (Result Http.Error TagEditResponse -> msg) -> Cmd msg
revertTags apiBase targets targetVersion toMsg =
    Http.post
        { url = tagsRevertEndpoint apiBase
        , body =
            Http.jsonBody
                (Encode.object
                    [ ( "posts", Encode.list encodeTarget targets )
                    , ( "target_version", Encode.int targetVersion )
                    ]
                )
        , expect = Http.expectJson toMsg tagEditResponseDecoder
        }


editReactions : String -> List ReactionTarget -> Maybe Bool -> Maybe Int -> (Result Http.Error ReactionResponse -> msg) -> Cmd msg
editReactions apiBase targets favorite score toMsg =
    Http.post
        { url = reactionsEndpoint apiBase
        , body =
            Http.jsonBody
                (Encode.object
                    ([ ( "posts", Encode.list encodeReactionTarget targets ) ]
                        ++ (case favorite of
                                Just value ->
                                    [ ( "favorite", Encode.bool value ) ]

                                Nothing ->
                                    []
                           )
                        ++ (case score of
                                Just value ->
                                    [ ( "score", Encode.int value ) ]

                                Nothing ->
                                    []
                           )
                    )
                )
        , expect = Http.expectJson toMsg reactionResponseDecoder
        }


encodeTarget : TagEditTarget -> Encode.Value
encodeTarget target =
    Encode.object
        [ ( "id", Encode.string target.id )
        , ( "version", Encode.int target.version )
        ]


encodeReactionTarget : ReactionTarget -> Encode.Value
encodeReactionTarget target =
    Encode.object
        [ ( "id", Encode.string target.id )
        , ( "version", Encode.int target.version )
        ]


endpoint : String -> String -> Maybe String -> Int -> String
endpoint apiBase query cursor limit =
    let
        params =
            [ Url.Builder.string "q" query
            , Url.Builder.int "limit" limit
            ]
                ++ (case cursor of
                        Just value ->
                            [ Url.Builder.string "cursor" value ]

                        Nothing ->
                            []
                   )
    in
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts" ] params

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts" ] params


detailEndpoint : String -> String -> String
detailEndpoint apiBase postId =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", postId ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", postId ] []


tagsEndpoint : String -> String
tagsEndpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "tags" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "tags" ] []


tagsRevertEndpoint : String -> String
tagsRevertEndpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "tags", "revert" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "tags", "revert" ] []


reactionsEndpoint : String -> String
reactionsEndpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "reactions" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "reactions" ] []


mediaUrl : String -> String -> String
mediaUrl apiBase value =
    if isAbsolute value || String.trim apiBase == "" then
        value

    else if String.startsWith "/" value then
        trimTrailingSlash apiBase ++ value

    else
        trimTrailingSlash apiBase ++ "/" ++ value


isAbsolute : String -> Bool
isAbsolute value =
    List.any (\prefix -> String.startsWith prefix value) [ "http://", "https://", "data:", "blob:" ]


trimTrailingSlash : String -> String
trimTrailingSlash value =
    if String.endsWith "/" value then
        String.dropRight 1 value

    else
        value
