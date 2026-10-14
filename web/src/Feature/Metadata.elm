module Feature.Metadata exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , SavePayload
    , activeChanged
    , artistDraft
    , conflict
    , context
    , errorToString
    , init
    , locked
    , requestId
    , saving
    , savePayload
    , sourceDraft
    , status
    , syncDetail
    , update
    )

{-| Single-post source/artist editing.

Drafts reset when the active post changes and sync from a newly loaded
detail. A save captures the post id, its explicit metadata version
(zero included), and the access generation; stale, wrong-resource, and
missing-result responses never settle the draft as saved. A conflict
keeps the draft and asks for an explicit reload instead of retrying.
-}

import Api.Error as ApiError
import Api.Metadata
import Domain.Post exposing (MetadataResponse, MetadataResult, PostDetail)
import Http


type alias Model =
    { sourceDraft : String
    , artistDraft : String
    , saving : Bool
    , status : Maybe String
    , requestId : Int
    , conflict : Bool
    , syncedId : Maybe String
    , dirty : Bool
    , reloadPending : Bool
    , baseSource : Maybe String
    , baseArtist : Maybe String
    , baseVersion : Maybe Int
    }


type alias SavePayload =
    { version : Int
    , source : Maybe String
    , artist : Maybe String
    }


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    , activeId : Maybe String
    , activeDetail : Maybe PostDetail
    }


context : String -> Maybe String -> Bool -> Int -> Maybe String -> Maybe PostDetail -> Context
context apiBase credential writesEnabled accessGeneration activeId activeDetail =
    { apiBase = apiBase
    , credential = credential
    , writesEnabled = writesEnabled
    , accessGeneration = accessGeneration
    , activeId = activeId
    , activeDetail = activeDetail
    }


type Msg
    = SourceChanged String
    | ArtistChanged String
    | SaveRequested
    | ReloadRequested
    | Completed Int Int String (Result Http.Error MetadataResponse)


type OutMsg
    = Saved MetadataResult
    | StaleResponse String
    | AccessRevoked
    | ReloadPost String


init : Model
init =
    { sourceDraft = ""
    , artistDraft = ""
    , saving = False
    , status = Nothing
    , requestId = 0
    , conflict = False
    , syncedId = Nothing
    , dirty = False
    , reloadPending = False
    , baseSource = Nothing
    , baseArtist = Nothing
    , baseVersion = Nothing
    }


sourceDraft : Model -> String
sourceDraft model =
    model.sourceDraft


artistDraft : Model -> String
artistDraft model =
    model.artistDraft


saving : Model -> Bool
saving model =
    model.saving


status : Model -> Maybe String
status model =
    model.status


conflict : Model -> Bool
conflict model =
    model.conflict


requestId : Model -> Int
requestId model =
    model.requestId


activeChanged : Model -> Model
activeChanged model =
    { model
        | sourceDraft = ""
        , artistDraft = ""
        , saving = False
        , status = Nothing
        , conflict = False
        , syncedId = Nothing
        , dirty = False
        , reloadPending = False
        , baseSource = Nothing
        , baseArtist = Nothing
        , baseVersion = Nothing
        , requestId = model.requestId + 1
    }


syncDetail : PostDetail -> Model -> Model
syncDetail detail model =
    if model.saving then
        model

    else if model.dirty && not model.reloadPending && model.syncedId == Just detail.id then
        model

    else
        { model
            | sourceDraft = detail.source
            , artistDraft = detail.artist
            , syncedId = Just detail.id
            , conflict = False
            , dirty = False
            , reloadPending = False
            , baseSource = Just detail.source
            , baseArtist = Just detail.artist
            , baseVersion = Just detail.metadataVersion
        }


savePayload : Model -> Maybe SavePayload
savePayload model =
    case ( model.baseVersion, model.baseSource, model.baseArtist ) of
        ( Just version, Just baseSource, Just baseArtist ) ->
            let
                source =
                    String.trim model.sourceDraft

                artist =
                    String.trim model.artistDraft
            in
            Just
                { version = version
                , source =
                    if source == baseSource then
                        Nothing

                    else
                        Just source
                , artist =
                    if artist == baseArtist then
                        Nothing

                    else
                        Just artist
                }

        _ ->
            Nothing


locked : Model -> Model
locked model =
    { model | saving = False, conflict = False, status = Nothing }


codePointLength : String -> Int
codePointLength value =
    String.toList value |> List.length


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        SourceChanged value ->
            ( { model | sourceDraft = value, status = Nothing, dirty = True }, Cmd.none, [] )

        ArtistChanged value ->
            ( { model | artistDraft = value, status = Nothing, dirty = True }, Cmd.none, [] )

        SaveRequested ->
            save ctx model

        ReloadRequested ->
            case ctx.activeId of
                Just postId ->
                    ( { model | conflict = False, reloadPending = True, status = Just "Reloading the post…" }, Cmd.none, [ ReloadPost postId ] )

                Nothing ->
                    ( model, Cmd.none, [] )

        Completed generation id postId result ->
            if generation /= ctx.accessGeneration then
                ( model, Cmd.none, [ StaleResponse postId ] )

            else if ctx.activeId /= Just postId then
                let
                    settled =
                        if id == model.requestId then
                            { model | saving = False }

                        else
                            model

                    outs =
                        case result of
                            Ok _ ->
                                [ StaleResponse postId ]

                            Err (Http.BadStatus 401) ->
                                [ AccessRevoked ]

                            Err _ ->
                                []
                in
                ( settled, Cmd.none, outs )

            else if id /= model.requestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok response ->
                        case List.filter (\post -> post.id == postId) response.posts |> List.head of
                            Just resultPost ->
                                ( { model
                                    | saving = False
                                    , sourceDraft = resultPost.source
                                    , artistDraft = resultPost.artist
                                    , conflict = False
                                    , dirty = False
                                    , reloadPending = False
                                    , syncedId = Just postId
                                    , baseSource = Just resultPost.source
                                    , baseArtist = Just resultPost.artist
                                    , baseVersion = Just resultPost.version
                                    , status =
                                        Just
                                            (if resultPost.changed then
                                                "Source and artist saved."

                                             else
                                                "No changes were needed."
                                            )
                                  }
                                , Cmd.none
                                , [ Saved resultPost ]
                                )

                            Nothing ->
                                ( { model
                                    | saving = False
                                    , status = Just "The server response did not include this post. Reload the post before saving again."
                                  }
                                , Cmd.none
                                , []
                                )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 409) ->
                        ( { model
                            | saving = False
                            , conflict = True
                            , status = Just "Source or artist changed elsewhere. Your draft is kept; reload the post before saving again."
                          }
                        , Cmd.none
                        , []
                        )

                    Err error ->
                        ( { model | saving = False, status = Just (errorToString error) }, Cmd.none, [] )


save : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
save ctx model =
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to edit source and artist." }, Cmd.none, [] )

    else if model.saving then
        ( model, Cmd.none, [] )

    else
        case ( ctx.activeId, ctx.activeDetail ) of
            ( Just postId, Just detail ) ->
                if detail.id /= postId || model.syncedId /= Just postId then
                    ( { model | status = Just "Wait for this post's details to load before saving." }, Cmd.none, [] )

                else
                    case savePayload model of
                        Nothing ->
                            ( { model | status = Just "Wait for this post's details to load before saving." }, Cmd.none, [] )

                        Just payload ->
                            let
                                source =
                                    String.trim model.sourceDraft

                                artist =
                                    String.trim model.artistDraft

                                hasChanges =
                                    payload.source /= Nothing || payload.artist /= Nothing
                            in
                            if not hasChanges then
                                ( { model | status = Just "Source and artist are unchanged." }, Cmd.none, [] )

                            else if codePointLength source > 512 || codePointLength artist > 512 then
                                ( { model | status = Just "Source and artist must be at most 512 characters each." }, Cmd.none, [] )

                            else
                                let
                                    id =
                                        model.requestId + 1
                                in
                                ( { model | saving = True, status = Nothing, conflict = False, requestId = id }
                                , Api.Metadata.edit ctx.apiBase
                                    ctx.credential
                                    [ { id = postId, version = payload.version } ]
                                    payload.source
                                    payload.artist
                                    (Completed ctx.accessGeneration id postId)
                                , []
                                )

            _ ->
                ( { model | status = Just "Select a post before editing source and artist." }, Cmd.none, [] )


errorToString : Http.Error -> String
errorToString error =
    case ApiError.classify error of
        ApiError.BadStatus 404 ->
            "This post no longer exists or is not visible."

        ApiError.BadStatus 403 ->
            "This credential cannot edit source and artist."

        ApiError.BadStatus code ->
            "Metadata edit failed (HTTP " ++ String.fromInt code ++ ")."

        ApiError.BadUrl url ->
            "Invalid API URL: " ++ url

        ApiError.Timeout ->
            "The metadata edit timed out. Reload the post before trying again."

        ApiError.NetworkError ->
            "The metadata server could not be reached."

        ApiError.BadBody ->
            "The metadata response was invalid. Reload the post before trying again."
