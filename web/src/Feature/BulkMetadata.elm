module Feature.BulkMetadata exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , artistDraft
    , baseVersion
    , captured
    , capturedTargets
    , changeArtist
    , changeSource
    , commonArtist
    , commonSource
    , conflict
    , context
    , desiredArtist
    , desiredSource
    , errorToString
    , init
    , knownSnapshot
    , protectDetail
    , loaded
    , loading
    , locked
    , needsReload
    , noteDetail
    , noteSnapshot
    , overLimit
    , ownerIds
    , readRequestId
    , requestId
    , reviewing
    , saving
    , sourceDraft
    , status
    , update
    , validateRead
    , validateReceipt
    , wantsRead
    )

{-| Bulk source/artist editing for a visible selection of 2..100 posts.

A compact snapshot read captures ids, source/artist values and metadata
versions (zero included) for the selection. Drafts name fields explicitly
through unchecked-by-default `Change source` / `Change artist` choices;
an unchecked field is omitted from the write and a checked empty field
clears. Base versions freeze once a draft exists: background detail or
snapshot reads update only the monotonic known cache and never rebase a
draft. A review must be confirmed before one atomic write; changing any
field, the selection or access invalidates that review. Conflicts and
uncertain outcomes preserve the desired edits, disable resubmission, and
require an explicit reload that recaptures versions before a new review.
A pending write stays targeted to its captured ids/versions even when
the selection moves; a late valid acknowledgement updates the known
cache but does not settle a newer selection's draft.
-}

import Api.Error as ApiError
import Api.Metadata
import Dict exposing (Dict)
import Domain.Post exposing (MetadataReadResponse, MetadataResponse, MetadataResult, MetadataSnapshot, MetadataTarget, PostDetail)
import Http
import Set


type alias Model =
    { ownerIds : List String
    , captured : List MetadataSnapshot
    , known : Dict String MetadataSnapshot
    , changeSource : Bool
    , changeArtist : Bool
    , sourceDraft : String
    , artistDraft : String
    , dirty : Bool
    , reviewing : Bool
    , saving : Bool
    , pendingTargets : List MetadataTarget
    , pendingSource : Maybe String
    , pendingArtist : Maybe String
    , pendingCaptured : List MetadataSnapshot
    , status : Maybe String
    , conflict : Bool
    , needsReload : Bool
    , readId : Int
    , writeId : Int
    , readLoading : Bool
    , readFailed : Bool
    , overLimit : Bool
    , awaitingReload : Bool
    }


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential writesEnabled accessGeneration =
    { apiBase = apiBase
    , credential = credential
    , writesEnabled = writesEnabled
    , accessGeneration = accessGeneration
    }


type Msg
    = SelectionSync (List String)
    | SourceCheckChanged Bool
    | ArtistCheckChanged Bool
    | SourceChanged String
    | ArtistChanged String
    | ReviewRequested
    | SaveRequested
    | ReloadRequested
    | ReadCompleted Int Int (List String) (Result Http.Error MetadataReadResponse)
    | WriteCompleted Int Int (List MetadataTarget) (Maybe String) (Maybe String) (List MetadataSnapshot) (Result Http.Error MetadataResponse)


type OutMsg
    = Saved (List MetadataResult) (List String)
    | Uncertain (List String)
    | StaleResponse (List String)
    | AccessRevoked


init : Model
init =
    { ownerIds = []
    , captured = []
    , known = Dict.empty
    , changeSource = False
    , changeArtist = False
    , sourceDraft = ""
    , artistDraft = ""
    , dirty = False
    , reviewing = False
    , saving = False
    , pendingTargets = []
    , pendingSource = Nothing
    , pendingArtist = Nothing
    , pendingCaptured = []
    , status = Nothing
    , conflict = False
    , needsReload = False
    , readId = 0
    , writeId = 0
    , readLoading = False
    , readFailed = False
    , overLimit = False
    , awaitingReload = False
    }


ownerIds : Model -> List String
ownerIds model =
    model.ownerIds


captured : Model -> List MetadataSnapshot
captured model =
    model.captured


sourceDraft : Model -> String
sourceDraft model =
    model.sourceDraft


artistDraft : Model -> String
artistDraft model =
    model.artistDraft


changeSource : Model -> Bool
changeSource model =
    model.changeSource


changeArtist : Model -> Bool
changeArtist model =
    model.changeArtist


reviewing : Model -> Bool
reviewing model =
    model.reviewing


saving : Model -> Bool
saving model =
    model.saving


status : Model -> Maybe String
status model =
    model.status


conflict : Model -> Bool
conflict model =
    model.conflict


needsReload : Model -> Bool
needsReload model =
    model.needsReload


overLimit : Model -> Bool
overLimit model =
    model.overLimit


loading : Model -> Bool
loading model =
    model.readLoading


loaded : Model -> Bool
loaded model =
    not (List.isEmpty model.ownerIds)
        && List.length model.captured == List.length model.ownerIds
        && List.all (\id -> List.any (\row -> row.id == id) model.captured) model.ownerIds


requestId : Model -> Int
requestId model =
    model.writeId


readRequestId : Model -> Int
readRequestId model =
    model.readId


capturedTargets : Model -> List MetadataTarget
capturedTargets model =
    model.pendingTargets


baseVersion : String -> Model -> Maybe Int
baseVersion postId model =
    model.captured
        |> List.filter (\row -> row.id == postId)
        |> List.head
        |> Maybe.map .version


knownSnapshot : String -> Model -> Maybe MetadataSnapshot
knownSnapshot postId model =
    Dict.get postId model.known


desiredSource : Model -> Maybe String
desiredSource model =
    if model.changeSource then
        Just (String.trim model.sourceDraft)

    else
        Nothing


desiredArtist : Model -> Maybe String
desiredArtist model =
    if model.changeArtist then
        Just (String.trim model.artistDraft)

    else
        Nothing


commonSource : Model -> Maybe String
commonSource model =
    common .source model


commonArtist : Model -> Maybe String
commonArtist model =
    common .artist model


common : (MetadataSnapshot -> String) -> Model -> Maybe String
common field model =
    case model.captured of
        first :: rest ->
            if List.all (\row -> field row == field first) rest then
                Just (field first)

            else
                Nothing

        [] ->
            Nothing


wantsRead : Model -> Bool
wantsRead model =
    List.length model.ownerIds >= 2
        && List.length model.ownerIds <= 100
        && not model.readLoading
        && not model.readFailed
        && not model.overLimit
        && not (loaded model)


{-| Lock/revoke: private snapshots, drafts and confirmations clear and
in-flight read/write responses go stale through bumped request ids.
-}
locked : Model -> Model
locked model =
    { init | readId = model.readId + 1, writeId = model.writeId + 1 }


codePointLength : String -> Int
codePointLength value =
    String.toList value |> List.length


hasNul : String -> Bool
hasNul value =
    String.contains "\u{0000}" value


fieldValid : String -> Bool
fieldValid value =
    codePointLength value <= 512 && not (hasNul value)


canonicalPositive : String -> Bool
canonicalPositive id =
    case String.toInt id of
        Just value ->
            value > 0 && String.fromInt value == id

        Nothing ->
            False


{-| A snapshot/detail pair is kept whole and monotonic: an older row
never regresses a newer known source/artist/version triple.
-}
noteSnapshot : MetadataSnapshot -> Model -> Model
noteSnapshot snapshot model =
    let
        noted =
            case Dict.get snapshot.id model.known of
                Just known ->
                    if snapshot.version < known.version then
                        model

                    else
                        { model | known = Dict.insert snapshot.id snapshot model.known }

                Nothing ->
                    { model | known = Dict.insert snapshot.id snapshot model.known }

        effective =
            effectiveSnapshot noted snapshot
    in
    if Set.member snapshot.id (Set.fromList model.ownerIds) && model.awaitingReload then
        { noted
            | captured =
                List.map
                    (\row ->
                        if row.id == snapshot.id && effective.version >= row.version then
                            effective

                        else
                            row
                    )
                    noted.captured
        }

    else if not noted.dirty && List.any (\row -> row.id == snapshot.id) noted.captured then
        { noted
            | captured =
                List.map
                    (\row ->
                        if row.id == snapshot.id && effective.version >= row.version then
                            effective

                        else
                            row
                    )
                    noted.captured
        }

    else if noted.dirty
        && (case baseRow snapshot.id noted of
                Just base ->
                    effective.version > base.version

                Nothing ->
                    False
           )
    then
        { noted
            | conflict = True
            , needsReload = True
            , reviewing = False
            , status = Just "Source or artist changed elsewhere. Your edits are kept; reload the selected images before saving again."
        }

    else
        noted


{-| A detail request may finish after a newer snapshot or write receipt.
Keep the authoritative source, artist and version together in the Inspector.
An old history cannot describe the newer value, so a fresh read fills it.
-}
protectDetail : Model -> PostDetail -> PostDetail
protectDetail model detail =
    case knownSnapshot detail.id model of
        Just known ->
            if known.version > detail.metadataVersion then
                { detail | source = known.source, artist = known.artist, metadataVersion = known.version, metadataHistory = [] }

            else
                detail

        Nothing ->
            detail


baseRow : String -> Model -> Maybe MetadataSnapshot
baseRow postId model =
    model.captured |> List.filter (\row -> row.id == postId) |> List.head


noteDetail : PostDetail -> Model -> Model
noteDetail detail model =
    noteSnapshot
        { id = detail.id
        , version = detail.metadataVersion
        , source = detail.source
        , artist = detail.artist
        }
        model


effectiveSnapshot : Model -> MetadataSnapshot -> MetadataSnapshot
effectiveSnapshot model snapshot =
    case Dict.get snapshot.id model.known of
        Just known ->
            if known.version > snapshot.version then
                known

            else
                snapshot

        Nothing ->
            snapshot


validateRead : List String -> MetadataReadResponse -> Bool
validateRead ids response =
    List.length response.posts
        == List.length ids
        && List.sort (List.map .id response.posts)
        == List.sort ids
        && List.all (\row -> canonicalPositive row.id && row.version >= 0) response.posts


{-| A success receipt must name exactly the submitted ids once each,
use canonical positive ids, return the desired value for every checked
field, keep every unchecked field at its captured original, and report
`base + 1` for a change or `base` for a no-op (where change is judged
against the captured originals). Anything else is uncertain.
-}
validateReceipt : List MetadataTarget -> Maybe String -> Maybe String -> List MetadataSnapshot -> MetadataResponse -> Bool
validateReceipt targets source artist originals response =
    let
        submittedIds =
            List.map .id targets

        returnedIds =
            List.map .id response.posts
    in
    List.length response.posts
        == List.length targets
        && List.sort returnedIds
        == List.sort submittedIds
        && List.all
            (\post ->
                case
                    ( List.filter (\target -> target.id == post.id) targets |> List.head
                    , List.filter (\row -> row.id == post.id) originals |> List.head
                    )
                of
                    ( Just target, Just original ) ->
                        let
                            expectedSource =
                                Maybe.withDefault original.source source

                            expectedArtist =
                                Maybe.withDefault original.artist artist

                            expectedChanged =
                                expectedSource /= original.source || expectedArtist /= original.artist
                        in
                        canonicalPositive post.id
                            && post.source
                            == expectedSource
                            && post.artist
                            == expectedArtist
                            && post.changed
                            == expectedChanged
                            && post.version
                            == (if post.changed then
                                    target.version + 1

                                else
                                    target.version
                               )

                    _ ->
                        False
            )
            response.posts


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        SelectionSync ids ->
            selectionSync ctx ids model

        SourceCheckChanged checked ->
            ( { model | changeSource = checked, dirty = True, reviewing = False, status = Nothing }, Cmd.none, [] )

        ArtistCheckChanged checked ->
            ( { model | changeArtist = checked, dirty = True, reviewing = False, status = Nothing }, Cmd.none, [] )

        SourceChanged value ->
            ( { model | sourceDraft = value, dirty = True, reviewing = False, status = Nothing }, Cmd.none, [] )

        ArtistChanged value ->
            ( { model | artistDraft = value, dirty = True, reviewing = False, status = Nothing }, Cmd.none, [] )

        ReviewRequested ->
            case validateDraft ctx model of
                Just message ->
                    ( { model | reviewing = False, status = Just message }, Cmd.none, [] )

                Nothing ->
                    ( { model | reviewing = True, status = Nothing }, Cmd.none, [] )

        SaveRequested ->
            save ctx model

        ReloadRequested ->
            if List.length model.ownerIds < 2 || List.length model.ownerIds > 100 || model.saving then
                ( model, Cmd.none, [] )

            else if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to edit source and artist." }, Cmd.none, [] )

            else
                startRead ctx model.ownerIds
                    { model
                        | awaitingReload = True
                        , reviewing = False
                        , readFailed = False
                        , status = Just "Reloading the selected images…"
                    }

        ReadCompleted generation rid ids result ->
            readCompleted ctx generation rid ids result model

        WriteCompleted generation wid targets source artist originals result ->
            if generation /= ctx.accessGeneration then
                ( model, Cmd.none, [ StaleResponse (List.map .id targets) ] )

            else if wid /= model.writeId then
                ( model, Cmd.none, [] )

            else
                settle targets source artist originals result model


selectionSync : Context -> List String -> Model -> ( Model, Cmd Msg, List OutMsg )
selectionSync ctx ids model =
    if ids == model.ownerIds then
        if model.readLoading || model.readFailed || model.overLimit || loaded model || List.length ids < 2 then
            ( model, Cmd.none, [] )

        else if not ctx.writesEnabled then
            ( { model | status = Just "Unlock to edit source and artist." }, Cmd.none, [] )

        else
            startRead ctx ids model

    else
        let
            over =
                List.length ids > 100

            cleared =
                { model
                    | ownerIds = ids
                    , captured = []
                    , changeSource = False
                    , changeArtist = False
                    , sourceDraft = ""
                    , artistDraft = ""
                    , dirty = False
                    , reviewing = False
                    , conflict = False
                    , needsReload = False
                    , readLoading = False
                    , readFailed = False
                    , overLimit = over
                    , awaitingReload = False
                    , status =
                        if over then
                            Just ("Select at most 100 images to edit source and artist at once (" ++ String.fromInt (List.length ids) ++ " selected). Editing is disabled; nothing was truncated.")

                        else
                            Nothing
                    , readId = model.readId + 1
                }
        in
        if over || List.length ids < 2 then
            ( cleared, Cmd.none, [] )

        else if not ctx.writesEnabled then
            ( { cleared | status = Just "Unlock to edit source and artist." }, Cmd.none, [] )

        else
            startRead ctx ids cleared


startRead : Context -> List String -> Model -> ( Model, Cmd Msg, List OutMsg )
startRead ctx ids model =
    let
        rid =
            model.readId + 1
    in
    ( { model | readId = rid, readLoading = True, readFailed = False }
    , Api.Metadata.read ctx.apiBase ctx.credential ids (ReadCompleted ctx.accessGeneration rid ids)
    , []
    )


readCompleted : Context -> Int -> Int -> List String -> Result Http.Error MetadataReadResponse -> Model -> ( Model, Cmd Msg, List OutMsg )
readCompleted ctx generation rid ids result model =
    if generation /= ctx.accessGeneration then
        ( model, Cmd.none, [] )

    else if rid /= model.readId then
        ( model, Cmd.none, [] )

    else if ids /= model.ownerIds then
        ( model, Cmd.none, [] )

    else
        case result of
            Ok response ->
                if not (validateRead ids response) then
                    ( { model
                        | readLoading = False
                        , readFailed = True
                        , awaitingReload = False
                        , reviewing = False
                        , status = Just "The selected images could not be read: the server response did not match the selection. Reload the selected images before editing."
                      }
                    , Cmd.none
                    , []
                    )

                else
                    let
                        noted =
                            List.foldl noteSnapshotOnlyKnown model response.posts

                        ordered =
                            ids
                                |> List.filterMap
                                    (\id ->
                                        response.posts
                                            |> List.filter (\row -> row.id == id)
                                            |> List.head
                                            |> Maybe.map (effectiveSnapshot noted)
                                    )
                    in
                    if model.awaitingReload then
                        ( { noted
                            | captured = ordered
                            , readLoading = False
                            , awaitingReload = False
                            , conflict = False
                            , needsReload = False
                            , reviewing = False
                            , status = Just "Selected images reloaded. Review your edits before saving."
                          }
                        , Cmd.none
                        , []
                        )

                    else if model.dirty then
                        -- A background snapshot never rebases a draft.
                        ( { noted | readLoading = False }, Cmd.none, [] )

                    else
                        ( { noted | captured = ordered, readLoading = False, status = Nothing }
                        , Cmd.none
                        , []
                        )

            Err (Http.BadStatus 401) ->
                ( { model | readLoading = False }, Cmd.none, [ AccessRevoked ] )

            Err (Http.BadStatus 404) ->
                ( { model
                    | readLoading = False
                    , readFailed = True
                    , awaitingReload = False
                    , status = Just "One selected image no longer exists or is not visible."
                  }
                , Cmd.none
                , []
                )

            Err error ->
                ( { model
                    | readLoading = False
                    , readFailed = True
                    , awaitingReload = False
                    , status = Just (errorToString error)
                  }
                , Cmd.none
                , []
                )


{-| Update only the known cache (used while settling a read so the read
itself cannot rebase a dirty captured base through `noteSnapshot`).
-}
noteSnapshotOnlyKnown : MetadataSnapshot -> Model -> Model
noteSnapshotOnlyKnown snapshot model =
    case Dict.get snapshot.id model.known of
        Just known ->
            if snapshot.version < known.version then
                model

            else
                { model | known = Dict.insert snapshot.id snapshot model.known }

        Nothing ->
            { model | known = Dict.insert snapshot.id snapshot model.known }


validateDraft : Context -> Model -> Maybe String
validateDraft ctx model =
    if model.saving then
        Just "Saving source and artist…"

    else if not ctx.writesEnabled then
        Just "Unlock to edit source and artist."

    else if model.overLimit || List.length model.ownerIds > 100 then
        Just "Select at most 100 images to edit source and artist at once. Editing is disabled; nothing was truncated."

    else if List.length model.ownerIds < 2 then
        Just "Select at least two images for bulk editing."

    else if model.needsReload || model.conflict then
        Just "Reload the selected images before saving again."

    else if model.readLoading then
        Just "Wait for the selected images to load before saving."

    else if not (loaded model) then
        Just "Wait for the selected images to load before saving."

    else if not model.changeSource && not model.changeArtist then
        Just "Choose “Change source” or “Change artist” before reviewing."

    else if model.changeSource && not (fieldValid (desiredSource model |> Maybe.withDefault "")) then
        Just "Source must be at most 512 characters and contain no NUL."

    else if model.changeArtist && not (fieldValid (desiredArtist model |> Maybe.withDefault "")) then
        Just "Artist must be at most 512 characters and contain no NUL."

    else if not (hasEffectiveChange model) then
        Just "The chosen fields already match every selected image."

    else
        Nothing


hasEffectiveChange : Model -> Bool
hasEffectiveChange model =
    List.any
        (\row ->
            (model.changeSource && (desiredSource model |> Maybe.withDefault row.source) /= row.source)
                || (model.changeArtist && (desiredArtist model |> Maybe.withDefault row.artist) /= row.artist)
        )
        model.captured


save : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
save ctx model =
    if model.saving then
        ( model, Cmd.none, [] )

    else if not model.reviewing then
        ( { model | status = Just "Review the changes before saving." }, Cmd.none, [] )

    else
        case validateDraft ctx model of
            Just message ->
                ( { model | reviewing = False, status = Just message }, Cmd.none, [] )

            Nothing ->
                let
                    targets =
                        model.captured |> List.map (\row -> { id = row.id, version = row.version })

                    wid =
                        model.writeId + 1

                    source =
                        desiredSource model

                    artist =
                        desiredArtist model
                in
                ( { model
                    | saving = True
                    , reviewing = False
                    , status = Nothing
                    , conflict = False
                    , writeId = wid
                    , pendingTargets = targets
                    , pendingSource = source
                    , pendingArtist = artist
                    , pendingCaptured = model.captured
                  }
                , Api.Metadata.edit ctx.apiBase ctx.credential targets source artist
                    (WriteCompleted ctx.accessGeneration wid targets source artist model.captured)
                , []
                )


settle : List MetadataTarget -> Maybe String -> Maybe String -> List MetadataSnapshot -> Result Http.Error MetadataResponse -> Model -> ( Model, Cmd Msg, List OutMsg )
settle targets source artist originals result model =
    let
        capturedIds =
            List.map .id targets
    in
    case result of
        Ok response ->
            if validateReceipt targets source artist originals response then
                let
                    noted =
                        List.foldl
                            (\post acc ->
                                noteSnapshotOnlyKnown
                                    { id = post.id, version = post.version, source = post.source, artist = post.artist }
                                    acc
                            )
                            model
                            response.posts

                    effectiveResults =
                        response.posts
                            |> List.map
                                (\post ->
                                    let
                                        eff =
                                            effectiveSnapshot noted
                                                { id = post.id, version = post.version, source = post.source, artist = post.artist }
                                    in
                                    { post | version = eff.version, source = eff.source, artist = eff.artist }
                                )

                    staleAck =
                        List.any
                            (\post ->
                                Dict.get post.id model.known
                                    |> Maybe.map (\known -> known.version > post.version)
                                    |> Maybe.withDefault False
                            )
                            response.posts

                    stillDirty =
                        ownsCurrent model targets source artist
                            && staleAck
                            && List.any
                                (\post ->
                                    (case source of
                                        Just desired ->
                                            post.source /= desired

                                        Nothing ->
                                            False
                                    )
                                        || (case artist of
                                                Just desired ->
                                                    post.artist /= desired

                                                Nothing ->
                                                    False
                                           )
                                )
                                effectiveResults

                    owns =
                        ownsCurrent model targets source artist
                in
                if stillDirty then
                    ( { noted
                        | saving = False
                        , pendingTargets = []
                        , pendingSource = Nothing
                        , pendingArtist = Nothing
                        , pendingCaptured = []
                        , conflict = True
                        , needsReload = True
                        , reviewing = False
                        , status = Just "Source or artist saved, but a newer value is now shown. Your edits are kept; reload the selected images before saving again."
                      }
                    , Cmd.none
                    , [ Saved effectiveResults capturedIds ]
                    )

                else if owns then
                    ( { noted
                        | saving = False
                        , pendingTargets = []
                        , pendingSource = Nothing
                        , pendingArtist = Nothing
                        , pendingCaptured = []
                        , captured =
                            List.map
                                (\resultPost ->
                                    { id = resultPost.id
                                    , version = resultPost.version
                                    , source = resultPost.source
                                    , artist = resultPost.artist
                                    }
                                )
                                effectiveResults
                        , changeSource = False
                        , changeArtist = False
                        , sourceDraft = ""
                        , artistDraft = ""
                        , dirty = False
                        , conflict = False
                        , needsReload = False
                        , status = Just ("Source and artist saved for " ++ String.fromInt (List.length capturedIds) ++ " images.")
                      }
                    , Cmd.none
                    , [ Saved effectiveResults capturedIds ]
                    )

                else
                    ( { noted
                        | saving = False
                        , pendingTargets = []
                        , pendingSource = Nothing
                        , pendingArtist = Nothing
                        , pendingCaptured = []
                      }
                    , Cmd.none
                    , [ Saved effectiveResults capturedIds ]
                    )

            else
                uncertain targets "the server response did not match the request." model

        Err (Http.BadStatus 401) ->
            ( model, Cmd.none, [ AccessRevoked ] )

        Err (Http.BadStatus 409) ->
            ( { model
                | saving = False
                , pendingTargets = []
                , pendingSource = Nothing
                , pendingArtist = Nothing
                , pendingCaptured = []
                , conflict = True
                , needsReload = True
                , reviewing = False
                , status = Just "Source or artist changed elsewhere. Your edits are kept; reload the selected images before saving again."
              }
            , Cmd.none
            , []
            )

        Err (Http.BadStatus 404) ->
            ( { model
                | saving = False
                , pendingTargets = []
                , pendingSource = Nothing
                , pendingArtist = Nothing
                , pendingCaptured = []
                , needsReload = True
                , reviewing = False
                , status = Just "One selected image no longer exists or is not visible. Reload the selected images before saving again."
              }
            , Cmd.none
            , []
            )

        Err (Http.BadStatus 400) ->
            ( clearedPending model (Just "The source and artist request was invalid."), Cmd.none, [] )

        Err (Http.BadStatus 403) ->
            ( clearedPending model (Just "This credential cannot edit source and artist."), Cmd.none, [] )

        Err error ->
            case ApiError.classify error of
                ApiError.BadStatus code ->
                    if code >= 500 then
                        uncertain targets ("the server returned HTTP " ++ String.fromInt code ++ ".") model

                    else
                        ( clearedPending model (Just (errorToString error)), Cmd.none, [] )

                _ ->
                    uncertain targets (errorToString error) model


ownsCurrent : Model -> List MetadataTarget -> Maybe String -> Maybe String -> Bool
ownsCurrent model targets source artist =
    model.ownerIds
        == List.map .id targets
        && desiredSource model
        == source
        && desiredArtist model
        == artist


clearedPending : Model -> Maybe String -> Model
clearedPending model message =
    { model
        | saving = False
        , pendingTargets = []
        , pendingSource = Nothing
        , pendingArtist = Nothing
        , pendingCaptured = []
        , reviewing = False
        , status = message
    }


uncertain : List MetadataTarget -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
uncertain targets reason model =
    ( { model
        | saving = False
        , pendingTargets = []
        , pendingSource = Nothing
        , pendingArtist = Nothing
        , pendingCaptured = []
        , needsReload = True
        , reviewing = False
        , status = Just ("The source and artist save result is uncertain: " ++ reason ++ " The selected images were refreshed; reload them before saving again.")
      }
    , Cmd.none
    , [ Uncertain (List.map .id targets) ]
    )


errorToString : Http.Error -> String
errorToString error =
    case ApiError.classify error of
        ApiError.BadStatus 404 ->
            "One selected image no longer exists or is not visible."

        ApiError.BadStatus 403 ->
            "This credential cannot edit source and artist."

        ApiError.BadStatus code ->
            "Source and artist edit failed (HTTP " ++ String.fromInt code ++ ")."

        ApiError.BadUrl url ->
            "Invalid API URL: " ++ url

        ApiError.Timeout ->
            "The source and artist edit timed out."

        ApiError.NetworkError ->
            "The source and artist server could not be reached."

        ApiError.BadBody ->
            "The source and artist response was invalid."
